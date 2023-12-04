package blockbuilder

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/NethermindEth/juno/adapters/sn2core"
	"github.com/NethermindEth/juno/blockbuilder/vm2core"
	"github.com/NethermindEth/juno/blockchain"
	"github.com/NethermindEth/juno/core"
	"github.com/NethermindEth/juno/core/crypto"
	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/db"
	"github.com/NethermindEth/juno/mempool"
	"github.com/NethermindEth/juno/starknet"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/juno/vm"
)

var (
	//go:embed account.json
	accountClassString string
	//go:embed erc20.json
	erc20ClassString string
	//go:embed udc.json
	udcClassString string

	sequencerAddress = new(felt.Felt)
)

func hexToFelt(hex string) *felt.Felt {
	result, err := new(felt.Felt).SetString(hex)
	if err != nil {
		panic(fmt.Errorf("string %s to felt: %v", hex, err))
	}
	return result
}

type Builder struct {
	chain      *blockchain.Blockchain
	starknetVM vm.VM
	mempool    *mempool.Mempool
}

func New(chain *blockchain.Blockchain, starknetVM vm.VM, mpool *mempool.Mempool) *Builder {
	return &Builder{
		chain:      chain,
		starknetVM: starknetVM,
		mempool:    mpool,
	}
}

func (b *Builder) storeGenesisBlockAndState() error { //nolint:funlen,gocyclo
	// Initialise values.

	udcAddress, err := new(felt.Felt).SetString("0x041a78e741e5af2fec34b695679bc6891742439f7afb8484ecd7766661ad02bf")
	if err != nil {
		return fmt.Errorf("set udc address: %v", err)
	}
	feeTokenAddress, err := new(felt.Felt).SetString("0x049d36570d4e46f48e99674bd3fcc84644ddd6b96f7c741b1562b82f9e004dc7")
	if err != nil {
		return fmt.Errorf("set fee token address: %v", err)
	}

	accountClassHash, err := new(felt.Felt).SetString("0x04d07e40e93398ed3c76981e72dd1fd22557a78ce36c0515f679e27f0bb5bc5f")
	if err != nil {
		return fmt.Errorf("set account contract class hash: %v", err)
	}
	erc20ClassHash, err := new(felt.Felt).SetString("0x02a8846878b6ad1f54f6ba46f5f40e11cee755c677f130b2c4b60566c9003f1f")
	if err != nil {
		return fmt.Errorf("set erc20 class hash: %v", err)
	}
	udcClassHash, err := new(felt.Felt).SetString("0x07b3e05f48f0c69e4a65ce5e076a66271a527aff2c34ce1083ec6e1526997a69")
	if err != nil {
		return fmt.Errorf("set udc class hash: %v", err)
	}

	classJSON := json.RawMessage{}

	if err = json.Unmarshal([]byte(accountClassString), &classJSON); err != nil {
		return fmt.Errorf("unmarshal account class: %v", err)
	}
	accountClass, err := adaptDeclaredClass(classJSON)
	if err != nil {
		return fmt.Errorf("adapt account class: %v", err)
	}

	if err = json.Unmarshal([]byte(erc20ClassString), &classJSON); err != nil {
		return fmt.Errorf("unmarshal erc20 class: %v", err)
	}
	erc20Class, err := adaptDeclaredClass(classJSON)
	if err != nil {
		return fmt.Errorf("adapt erc20 class: %v", err)
	}

	if err = json.Unmarshal([]byte(udcClassString), &classJSON); err != nil {
		return fmt.Errorf("unmarshal udc class: %v", err)
	}
	udcClass, err := adaptDeclaredClass(classJSON)
	if err != nil {
		return fmt.Errorf("adapt udc class: %v", err)
	}

	testAddress := hexToFelt("0x2")

	defaultPrefundedAccountBalance, err := new(felt.Felt).SetString("0x3635c9adc5dea00000") // 10^21
	if err != nil {
		return fmt.Errorf("default prefunded account balance: %v", err)
	}

	erc20NameStorageSlot, err := new(felt.Felt).SetString("0x0341c1bdfd89f69748aa00b5742b03adbffd79b8e80cab5c50d91cd8c2a79be1")
	if err != nil {
		return fmt.Errorf("erc20 name storage slot: %v", err)
	}
	erc20SymbolStorageSlot, err := new(felt.Felt).SetString("0x00b6ce5410fca59d078ee9b2a4371a9d684c530d697c64fbef0ae6d5e8f0ac72")
	if err != nil {
		return fmt.Errorf("erc20 symbol storage slot: %v", err)
	}
	erc20DecimalsStorageSlot, err := new(felt.Felt).SetString("0x01f0d4aa99431d246bac9b8e48c33e888245b15e9678f64f9bdfc8823dc8f979")
	if err != nil {
		return fmt.Errorf("erc20 decimals storage slot: %v", err)
	}

	// Store genesis state.

	stateRoot := hexToFelt("0xd149d5719ffbce57fca2673f412b88565f1c2ebf37b570efc3034b18545c45")
	emptyReceipts := []*core.TransactionReceipt{}
	block := &core.Block{
		Header: &core.Header{
			Hash:             nil,
			GlobalStateRoot:  stateRoot,
			ParentHash:       new(felt.Felt),
			SequencerAddress: new(felt.Felt),
			TransactionCount: 0,
			EventCount:       0,
			ProtocolVersion:  "v0.12.3",
			GasPrice:         new(felt.Felt),
			Timestamp:        uint64(time.Now().Unix()),
			EventsBloom:      core.EventsBloom(emptyReceipts),
		},
		Transactions: []core.Transaction{},
		Receipts:     emptyReceipts,
	}
	blockHash, commitments, err := core.BlockHash(block, utils.Goerli2, sequencerAddress)
	if err != nil {
		return fmt.Errorf("genesis block hash: %v", err)
	}
	block.Hash = blockHash
	if err = b.chain.Store(block, commitments, &core.StateUpdate{
		BlockHash: blockHash,
		NewRoot:   stateRoot,
		OldRoot:   new(felt.Felt),
		StateDiff: &core.StateDiff{
			StorageDiffs: map[felt.Felt][]core.StorageDiff{
				*feeTokenAddress: {
					{Key: erc20NameStorageSlot, Value: hexToFelt("0x4574686572")},
					{Key: erc20SymbolStorageSlot, Value: hexToFelt("0x455448")},
					{Key: erc20DecimalsStorageSlot, Value: new(felt.Felt).SetUint64(18)}, //nolint:gomnd
					{Key: getStorageVarAddress("ERC20_balances", testAddress), Value: defaultPrefundedAccountBalance},
				},
				*testAddress: {
					{
						Key:   hexToFelt("0x01379ac0624b939ceb9dede92211d7db5ee174fe28be72245b0a1a2abd81c98f"),
						Value: hexToFelt("0x043661740237e2be32500042dbd2afda8ab94ad11d6cea9da379ee5de3d376a2"),
					},
				},
			},
			Nonces: map[felt.Felt]*felt.Felt{
				*feeTokenAddress: new(felt.Felt).SetUint64(1),
				*udcAddress:      new(felt.Felt).SetUint64(1),
				*testAddress:     new(felt.Felt).SetUint64(1),
			},
			DeployedContracts: []core.AddressClassHashPair{
				{Address: feeTokenAddress, ClassHash: erc20ClassHash},
				{Address: udcAddress, ClassHash: udcClassHash},
				{Address: testAddress, ClassHash: accountClassHash},
			},
			DeclaredV0Classes: []*felt.Felt{accountClassHash, erc20ClassHash, udcClassHash}, // Doesn't do anything.
			DeclaredV1Classes: []core.DeclaredV1Class{},
			ReplacedClasses:   []core.AddressClassHashPair{},
		},
	}, map[felt.Felt]core.Class{
		*accountClassHash: accountClass,
		*erc20ClassHash:   erc20Class,
		*udcClassHash:     udcClass,
	}); err != nil {
		return fmt.Errorf("store: %v", err)
	}

	return nil
}

// Run(ctx context.Context) defines blockbuilder as a Service.Service
func (b *Builder) Run(ctx context.Context) error {
	if _, err := b.chain.Height(); err != nil {
		if !errors.Is(err, db.ErrKeyNotFound) {
			return fmt.Errorf("chain height: %v", err)
		}

		if err := b.storeGenesisBlockAndState(); err != nil {
			return fmt.Errorf("store genesis block and state: %v", err)
		}
	}

	for ctx.Err() == nil {
		curHeader, err := b.chain.HeadsHeader()
		if err != nil {
			return fmt.Errorf("heads header: %v", err)
		}

		txn := b.mempool.Dequeue()
		if txn == nil {
			time.Sleep(time.Second)
			continue
		}
		classes := []core.Class{}
		switch txn.Class.(type) {
		case *core.Cairo0Class, *core.Cairo1Class:
			classes = []core.Class{txn.Class}
		}
		declaredClasses := map[felt.Felt]core.Class{}
		if snTx, ok := txn.Transaction.(*core.DeclareTransaction); ok {
			declaredClasses[*snTx.ClassHash] = txn.Class
		}

		singleReceipt := []*core.TransactionReceipt{{
			TransactionHash: txn.Transaction.Hash(),
		}}
		pendingHeader := &core.Header{
			ParentHash:       curHeader.Hash,
			Number:           curHeader.Number + 1,
			GlobalStateRoot:  new(felt.Felt), // TODO set root
			SequencerAddress: curHeader.SequencerAddress,
			TransactionCount: 1,
			Timestamp:        uint64(time.Now().Unix()),
			ProtocolVersion:  curHeader.ProtocolVersion,
			GasPrice:         new(felt.Felt),
			EventCount:       0,                               // TODO set events
			EventsBloom:      core.EventsBloom(singleReceipt), // TODO set event blooms
		}

		txs := []core.Transaction{txn.Transaction}
		stateReader, stateCloser, err := b.chain.HeadState()
		if err != nil {
			return fmt.Errorf("head state: %v", err)
		}
		_, traces, err := b.starknetVM.Execute(
			txs,
			classes,
			pendingHeader.Number,
			pendingHeader.Timestamp,
			pendingHeader.SequencerAddress,
			stateReader,
			b.chain.Network(),
			[]*felt.Felt{},
			false,
			false,
			new(felt.Felt),
			new(felt.Felt),
			false,
		)
		if err != nil {
			return fmt.Errorf("execute transaction: %v", err)
		}
		if err = stateCloser(); err != nil {
			return fmt.Errorf("close state: %v", err)
		}

		stateDiff, err := vm2core.TraceToStateDiff(traces[0])
		if err != nil {
			return fmt.Errorf("trace to state diff: %v", err)
		}
		block := &core.Block{
			Header:       pendingHeader,
			Receipts:     singleReceipt, // TODO set receipts
			Transactions: txs,
		}
		blockHash, commitments, err := core.BlockHash(block, utils.Goerli2, sequencerAddress)
		if err != nil {
			return fmt.Errorf("block hash: %v", err)
		}
		block.Hash = blockHash
		if err = b.chain.Store(block, commitments, &core.StateUpdate{
			BlockHash: blockHash,
			// TODO. There isn't a good way to get this when we're sequencing. We need to refactor core/state.go and core/state_update.go.
			NewRoot:   new(felt.Felt),
			OldRoot:   curHeader.GlobalStateRoot,
			StateDiff: stateDiff,
		}, declaredClasses); err != nil {
			return fmt.Errorf("store: %v", err)
		}
		fmt.Printf("stored block %d\n", block.Number)
		fmt.Printf("  transaction hash: %s\n", txn.Transaction.Hash())
	}
	return nil
}

var (
	patiricaUpperBound  = hexToFelt("0x800000000000000000000000000000000000000000000000000000000000000") // 2^251
	l2AddressUpperBound = new(felt.Felt).Sub(patiricaUpperBound, new(felt.Felt).SetUint64(256))          //nolint:gomnd
)

func getStorageVarAddress(name string, args ...*felt.Felt) *felt.Felt {
	nameKeccak, err := crypto.StarknetKeccak([]byte(name))
	if err != nil {
		panic(fmt.Errorf("starknet keccak: %v", err))
	}
	x := crypto.PedersenArray(slices.Insert(args, 0, nameKeccak)...)
	if x.Cmp(l2AddressUpperBound) == -1 {
		return x
	}
	return new(felt.Felt).Sub(x, l2AddressUpperBound)
}

/*
starkli deploy --account myaccount 0x04d07e40e93398ed3c76981e72dd1fd22557a78ce36c0515f679e27f0bb5bc5f --keystore keystore
*/

func adaptDeclaredClass(declaredClass json.RawMessage) (core.Class, error) {
	var feederClass starknet.ClassDefinition
	err := json.Unmarshal(declaredClass, &feederClass)
	if err != nil {
		return nil, err
	}

	switch {
	case feederClass.V1 != nil:
		return sn2core.AdaptCairo1Class(feederClass.V1, nil)
	case feederClass.V0 != nil:
		return sn2core.AdaptCairo0Class(feederClass.V0)
	default:
		return nil, errors.New("empty class")
	}
}
