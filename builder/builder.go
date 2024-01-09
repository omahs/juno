package builder

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/NethermindEth/juno/adapters/vm2core"
	"github.com/NethermindEth/juno/blockchain"
	"github.com/NethermindEth/juno/core"
	"github.com/NethermindEth/juno/core/crypto"
	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/db"
	"github.com/NethermindEth/juno/feed"
	"github.com/NethermindEth/juno/mempool"
	"github.com/NethermindEth/juno/service"
	"github.com/NethermindEth/juno/sync"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/juno/vm"
	"github.com/consensys/gnark-crypto/ecc/stark-curve/ecdsa"
)

var (
	_ service.Service = (*Builder)(nil)
	_ sync.Reader     = (*Builder)(nil)
)

type Builder struct {
	ownAddress felt.Felt
	privKey    *ecdsa.PrivateKey

	bc       *blockchain.Blockchain
	vm       vm.VM
	newHeads *feed.Feed[*core.Header]
	log      utils.Logger

	// Returns true when the builder should exit. Called after finalising each block.
	// Useful for tests.
	finaliseCb func() bool
	blockTime  time.Duration
}

func New(privKey *ecdsa.PrivateKey, ownAddr *felt.Felt, bc *blockchain.Blockchain, builderVM vm.VM,
	blockTime time.Duration, log utils.Logger,
) *Builder {
	return &Builder{
		ownAddress: *ownAddr,
		privKey:    privKey,
		blockTime:  blockTime,
		log:        log,

		bc:       bc,
		vm:       builderVM,
		newHeads: feed.New[*core.Header](),
	}
}

// WithFinisher is useful for testing. The function f does not need to be goroutine-safe.
func (b *Builder) WithFinaliseCb(cb func() bool) *Builder {
	b.finaliseCb = cb
	return b
}

// Run takes ownership of the Builder.
func (b *Builder) Run(ctx context.Context) error {
	ticker := time.NewTicker(b.blockTime)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := b.bc.Finalise([]core.Transaction{}, []*core.TransactionReceipt{}, core.EmptyStateDiff(), nil,
				&b.ownAddress, b.Sign); err != nil {
				return fmt.Errorf("finalise: %v", err)
			}
			header, err := b.bc.HeadsHeader()
			if err != nil {
				return fmt.Errorf("get header of latest block: %v", err)
			}
			b.log.Infow("Finalised block", "number", header.Number, "hash", header.Hash.ShortString())

			if b.finaliseCb != nil && b.finaliseCb() {
				return nil
			}
		}
	}
}

// ValidateAgainstPendingState validates a user transaction against the pending state
// only hard-failures result in an error, reverts are not reported back to caller
func (b *Builder) ValidateAgainstPendingState(userTxn *mempool.BroadcastedTransaction) error {
	declaredClasses := []core.Class{}
	if userTxn.DeclaredClass != nil {
		declaredClasses = []core.Class{userTxn.DeclaredClass}
	}

	nextHeight := uint64(0)
	if height, err := b.bc.Height(); err == nil {
		nextHeight = height + 1
	} else if err != nil && !errors.Is(err, db.ErrKeyNotFound) {
		return err
	}

	pendingBlock, err := b.bc.Pending()
	if err != nil {
		return err
	}

	state, stateCloser, err := b.bc.PendingState()
	if err != nil {
		return err
	}

	defer func() {
		if err = stateCloser(); err != nil {
			b.log.Errorw("closing state in ValidateAgainstPendingState", "err", err)
		}
	}()

	_, _, err = b.vm.Execute([]core.Transaction{userTxn.Transaction}, declaredClasses, nextHeight,
		pendingBlock.Block.Timestamp, &b.ownAddress, state, b.bc.Network(), []*felt.Felt{},
		false, false, false, pendingBlock.Block.GasPrice, pendingBlock.Block.GasPriceSTRK, false)
	return err
}

func (b *Builder) StartingBlockNumber() (uint64, error) {
	return 0, nil
}

func (b *Builder) HighestBlockHeader() *core.Header {
	return nil
}

func (b *Builder) SubscribeNewHeads() sync.HeaderSubscription {
	return sync.HeaderSubscription{
		Subscription: b.newHeads.Subscribe(),
	}
}

// Sign returns the builder's signature over data.
func (b *Builder) Sign(blockHash, stateDiffCommitment *felt.Felt) ([]*felt.Felt, error) {
	data := crypto.PoseidonArray(blockHash, stateDiffCommitment).Bytes()
	signatureBytes, err := b.privKey.Sign(data[:], nil)
	if err != nil {
		return nil, err
	}
	sig := make([]*felt.Felt, 0)
	for start := 0; start < len(signatureBytes); {
		step := len(signatureBytes[start:])
		if step > felt.Bytes {
			step = felt.Bytes
		}
		sig = append(sig, new(felt.Felt).SetBytes(signatureBytes[start:step]))
		start += step
	}
	return sig, nil
}

func Receipt(fee *felt.Felt, feeUnit core.FeeUnit, txHash *felt.Felt, trace *vm.TransactionTrace) *core.TransactionReceipt {
	return &core.TransactionReceipt{
		Fee:                fee,
		FeeUnit:            feeUnit,
		Events:             vm2core.AdaptOrderedEvents(trace.AllEvents()),
		ExecutionResources: vm2core.AdaptExecutionResources(trace.TotalExecutionResources()),
		L2ToL1Message:      vm2core.AdaptOrderedMessagesToL1(trace.AllMessages()),
		TransactionHash:    txHash,
		Reverted:           trace.IsReverted(),
		RevertReason:       trace.RevertReason(),
	}
}
