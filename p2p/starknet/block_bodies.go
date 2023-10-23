package starknet

import (
	"slices"

	"github.com/NethermindEth/juno/adapters/core2p2p"
	"github.com/NethermindEth/juno/blockchain"
	"github.com/NethermindEth/juno/core"
	"github.com/NethermindEth/juno/p2p/starknet/spec"
	"github.com/NethermindEth/juno/utils"
	"google.golang.org/protobuf/proto"
)

type streamState int

const (
	_ streamState = iota

	sendDiff // initial
	sendClasses
	sendBlockFin
	terminal // final state
)

type blockBodyIterator struct {
	bcReader blockchain.Reader
	log      utils.Logger

	state       streamState
	header      *core.Header
	stateUpdate *core.StateUpdate
}

func newBlockBodyIterator(bcReader blockchain.Reader, header *core.Header, log utils.Logger) *blockBodyIterator {
	return &blockBodyIterator{
		state:       sendDiff,
		header:      header,
		bcReader:    bcReader,
		log:         log,
		stateUpdate: nil, // to be filled during one of next states
	}
}

func (b *blockBodyIterator) hasNext() bool {
	statesHaveTransitions := []streamState{
		sendDiff,
		sendClasses,
		sendBlockFin,
	}
	return slices.Contains(statesHaveTransitions, b.state)
}

// Either BlockBodiesResponse_Diff, *_Classes, *_Proof, *_Fin
func (b *blockBodyIterator) next() (msg proto.Message, valid bool) {
	switch b.state {
	case sendDiff:
		msg, valid = b.diff()
		b.state = sendClasses
	case sendClasses:
		msg, valid = b.classes()
		b.state = sendBlockFin
	case sendBlockFin:
		msg, valid = b.fin()
		b.state = terminal
	case terminal:
		panic("next called on terminal state")
	default:
		b.log.Errorw("Unknown state in blockBodyIterator", "state", b.state)
	}

	return
}

func (b *blockBodyIterator) classes() (proto.Message, bool) {
	var classes []*spec.Class

	state, closer, err := b.bcReader.StateAtBlockNumber(b.header.Number)
	if err != nil {
		return b.finAndSetTerminalState()
	}
	defer b.callAndLogErr(closer, "Error closing state reader in blockBodyIterator.classes()")

	stateDiff := b.stateUpdate.StateDiff

	for _, hash := range stateDiff.DeclaredV0Classes {
		cls, err := state.Class(hash)
		if err != nil {
			return b.finAndSetTerminalState()
		}

		classes = append(classes, core2p2p.AdaptClass(cls.Class, hash))
	}
	for _, class := range stateDiff.DeclaredV1Classes {
		cls, err := state.Class(class.ClassHash)
		if err != nil {
			return b.finAndSetTerminalState()
		}

		cairo1Cls := cls.Class.(*core.Cairo1Class)
		classes = append(classes, core2p2p.AdaptClass(cls.Class, cairo1Cls.Hash()))
	}

	return &spec.BlockBodiesResponse{
		Id: core2p2p.AdaptBlockID(b.header),
		BodyMessage: &spec.BlockBodiesResponse_Classes{
			Classes: &spec.Classes{
				Domain:  0,
				Classes: classes,
			},
		},
	}, true
}

func (b *blockBodyIterator) diff() (proto.Message, bool) {
	var err error
	b.stateUpdate, err = b.bcReader.StateUpdateByNumber(b.header.Number)
	if err != nil {
		b.log.Errorw("Failed to get state", "err", err)
		return b.finAndSetTerminalState()
	}
	diff := b.stateUpdate.StateDiff

	var contractDiffs []*spec.StateDiff_ContractDiff
	contractPairs := utils.Flatten(diff.DeployedContracts, diff.ReplacedClasses)
	for _, pair := range contractPairs {
		addr := *pair.Address
		contractDiff := core2p2p.AdaptStateDiff(
			pair,
			diff.Nonces[addr],
			diff.StorageDiffs[addr],
		)

		contractDiffs = append(contractDiffs, contractDiff)
	}

	return &spec.BlockBodiesResponse{
		Id: core2p2p.AdaptBlockID(b.header),
		BodyMessage: &spec.BlockBodiesResponse_Diff{
			Diff: &spec.StateDiff{
				Domain:        0,
				ContractDiffs: contractDiffs,
			},
		},
	}, true
}

func (b *blockBodyIterator) fin() (proto.Message, bool) {
	return &spec.BlockBodiesResponse{
		Id:          core2p2p.AdaptBlockID(b.header),
		BodyMessage: &spec.BlockBodiesResponse_Fin{},
	}, true
}

func (b *blockBodyIterator) finAndSetTerminalState() (proto.Message, bool) {
	b.state = terminal
	return b.fin()
}

func (b *blockBodyIterator) callAndLogErr(f func() error, msg string) {
	err := f()
	if err != nil {
		b.log.Errorw(msg, "err", err)
	}
}
