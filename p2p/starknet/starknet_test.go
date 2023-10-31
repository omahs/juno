package starknet_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/NethermindEth/juno/adapters/core2p2p"
	"github.com/NethermindEth/juno/core"
	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/mocks"
	"github.com/NethermindEth/juno/p2p/starknet"
	"github.com/NethermindEth/juno/p2p/starknet/spec"
	"github.com/NethermindEth/juno/utils"
	"github.com/davecgh/go-spew/spew"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func nopCloser() error { return nil }

func TestClientHandler(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	testNetwork := utils.INTEGRATION
	testCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mockNet, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)

	peers := mockNet.Peers()
	require.Len(t, peers, 2)
	handlerID := peers[0]
	clientID := peers[1]

	log, err := utils.NewZapLogger(utils.ERROR, false)
	require.NoError(t, err)
	mockReader := mocks.NewMockReader(mockCtrl)
	handler := starknet.NewHandler(mockReader, log)

	handlerHost := mockNet.Host(handlerID)
	handlerHost.SetStreamHandler(starknet.BlockHeadersPID(testNetwork), handler.BlockHeadersHandler)
	handlerHost.SetStreamHandler(starknet.BlockBodiesPID(testNetwork), handler.BlockBodiesHandler)
	handlerHost.SetStreamHandler(starknet.EventsPID(testNetwork), handler.EventsHandler)
	handlerHost.SetStreamHandler(starknet.ReceiptsPID(testNetwork), handler.ReceiptsHandler)
	handlerHost.SetStreamHandler(starknet.TransactionsPID(testNetwork), handler.TransactionsHandler)

	clientHost := mockNet.Host(clientID)
	client := starknet.NewClient(func(ctx context.Context, pids ...protocol.ID) (network.Stream, error) {
		return clientHost.NewStream(ctx, handlerID, pids...)
	}, testNetwork, log)

	t.Run("get block headers", func(t *testing.T) {
		type pair struct {
			header      *core.Header
			commitments *core.BlockCommitments
		}
		pairsPerBlock := []pair{}
		for i := uint64(0); i < 2; i++ {
			pairsPerBlock = append(pairsPerBlock, pair{
				header: fillFelts(t, &core.Header{
					Number:           i,
					Timestamp:        i,
					TransactionCount: i,
					EventCount:       i,
				}),
				commitments: fillFelts(t, &core.BlockCommitments{}),
			})
		}

		for blockNumber, pair := range pairsPerBlock {
			blockNumber := uint64(blockNumber)
			mockReader.EXPECT().BlockHeaderByNumber(blockNumber).Return(pair.header, nil)
			mockReader.EXPECT().BlockCommitmentsByNumber(blockNumber).Return(pair.commitments, nil)
		}

		numOfBlocks := uint64(len(pairsPerBlock))
		res, cErr := client.RequestBlockHeaders(testCtx, &spec.BlockHeadersRequest{
			Iteration: &spec.Iteration{
				Start: &spec.Iteration_BlockNumber{
					BlockNumber: 0,
				},
				Direction: spec.Iteration_Forward,
				Limit:     numOfBlocks,
				Step:      1,
			},
		})
		require.NoError(t, cErr)

		var count uint64
		for response, valid := res(); valid; response, valid = res() {
			if count == numOfBlocks {
				assert.True(t, proto.Equal(&spec.Fin{}, response.Part[0].GetFin()))
				count++
				break
			}

			adaptHash := core2p2p.AdaptHash
			expectedPair := pairsPerBlock[count]
			header := expectedPair.header

			expectedResponse := &spec.BlockHeadersResponse{
				Part: []*spec.BlockHeadersResponsePart{
					{
						HeaderMessage: &spec.BlockHeadersResponsePart_Header{
							Header: &spec.BlockHeader{
								ParentHeader:     adaptHash(header.ParentHash),
								Number:           header.Number,
								Time:             timestamppb.New(time.Unix(int64(header.Timestamp), 0)),
								SequencerAddress: core2p2p.AdaptAddress(header.SequencerAddress),
								State: &spec.Patricia{
									Height: 251,
									Root:   adaptHash(header.GlobalStateRoot),
								},
								Transactions: &spec.Merkle{
									NLeaves: uint32(header.TransactionCount),
									Root:    adaptHash(expectedPair.commitments.TransactionCommitment),
								},
								Events: &spec.Merkle{
									NLeaves: uint32(header.EventCount),
									Root:    adaptHash(expectedPair.commitments.EventCommitment),
								},
							},
						},
					},
					{
						HeaderMessage: &spec.BlockHeadersResponsePart_Signatures{
							Signatures: &spec.Signatures{
								Block:      core2p2p.AdaptBlockID(expectedPair.header),
								Signatures: utils.Map(expectedPair.header.Signatures, core2p2p.AdaptSignature),
							},
						},
					},
				},
			}
			assert.True(t, proto.Equal(expectedResponse, response))

			assert.Equal(t, count, response.Part[0].GetHeader().Number)
			count++
		}

		expectedCount := numOfBlocks + 1 // plus fin
		require.Equal(t, expectedCount, count)
	})

	t.Run("get block bodies", func(t *testing.T) {
		deployedClassHash := utils.HexToFelt(t, "0XCAFEBABE")
		deployedAddress := utils.HexToFelt(t, "0XDEADBEEF")
		replacedClassHash := utils.HexToFelt(t, "0XABCD")
		replacedAddress := utils.HexToFelt(t, "0XABCDE")
		declaredV0Class := randFelt(t)
		storageDiff := core.StorageDiff{
			Key:   randFelt(t),
			Value: randFelt(t),
		}
		const (
			cairo0Program = "cairo_0_program"
			cairo1Program = "cairo_1_program"
		)
		cairo1Class := &core.Cairo1Class{
			AbiHash:     randFelt(t),
			ProgramHash: randFelt(t),
			Compiled:    json.RawMessage(cairo1Program),
		}

		blocks := []struct {
			number    uint64
			stateDiff *core.StateDiff
		}{
			{
				number: 1,
				stateDiff: &core.StateDiff{
					StorageDiffs: map[felt.Felt][]core.StorageDiff{
						*deployedAddress: {
							storageDiff,
						},
					},
					Nonces: map[felt.Felt]*felt.Felt{
						*deployedAddress: randFelt(t),
						*replacedAddress: randFelt(t),
					},
					DeployedContracts: []core.AddressClassHashPair{
						{
							Address:   deployedAddress,
							ClassHash: deployedClassHash,
						},
					},
					DeclaredV0Classes: []*felt.Felt{declaredV0Class},
					DeclaredV1Classes: []core.DeclaredV1Class{
						{
							ClassHash:         randFelt(t),
							CompiledClassHash: randFelt(t),
						},
					},
					ReplacedClasses: []core.AddressClassHashPair{
						{
							Address:   replacedAddress,
							ClassHash: replacedClassHash,
						},
					},
				},
			},
		}
		limit := uint64(len(blocks))

		for _, block := range blocks {
			mockReader.EXPECT().BlockHeaderByNumber(block.number).Return(&core.Header{
				Number: block.number,
			}, nil)

			mockReader.EXPECT().StateUpdateByNumber(block.number).Return(&core.StateUpdate{
				StateDiff: block.stateDiff,
			}, nil)

			stateHistory := mocks.NewMockStateHistoryReader(mockCtrl)
			v0Class := block.stateDiff.DeclaredV0Classes[0]
			stateHistory.EXPECT().Class(v0Class).Return(&core.DeclaredClass{
				At: block.number,
				Class: &core.Cairo0Class{
					Program: cairo0Program,
				},
			}, nil)
			v1Class := block.stateDiff.DeclaredV1Classes[0]
			stateHistory.EXPECT().Class(v1Class.ClassHash).Return(&core.DeclaredClass{
				At:    block.number,
				Class: cairo1Class,
			}, nil)

			stateHistory.EXPECT().ContractClassHash(deployedAddress).Return(deployedClassHash, nil).AnyTimes()
			stateHistory.EXPECT().ContractClassHash(replacedAddress).Return(replacedClassHash, nil).AnyTimes()

			mockReader.EXPECT().StateAtBlockNumber(block.number).Return(stateHistory, nopCloser, nil).Times(2)
		}

		res, cErr := client.RequestBlockBodies(testCtx, &spec.BlockBodiesRequest{
			Iteration: &spec.Iteration{
				Start: &spec.Iteration_BlockNumber{
					BlockNumber: blocks[0].number,
				},
				Direction: spec.Iteration_Forward,
				Limit:     limit,
				Step:      1,
			},
		})
		require.NoError(t, cErr)

		expectedMessages := []*spec.BlockBodiesResponse{
			{
				Id: &spec.BlockID{
					Number: blocks[0].number,
				},
				BodyMessage: &spec.BlockBodiesResponse_Diff{
					Diff: &spec.StateDiff{
						ContractDiffs: []*spec.StateDiff_ContractDiff{
							{
								Address:   core2p2p.AdaptAddress(deployedAddress),
								ClassHash: core2p2p.AdaptFelt(deployedClassHash),
								Nonce:     core2p2p.AdaptFelt(blocks[0].stateDiff.Nonces[*deployedAddress]),
								Values: []*spec.ContractStoredValue{
									{
										Key:   core2p2p.AdaptFelt(storageDiff.Key),
										Value: core2p2p.AdaptFelt(storageDiff.Value),
									},
								},
							},
							{
								Address:   core2p2p.AdaptAddress(replacedAddress),
								ClassHash: core2p2p.AdaptFelt(replacedClassHash),
								Nonce:     core2p2p.AdaptFelt(blocks[0].stateDiff.Nonces[*replacedAddress]),
							},
						},
					},
				},
			},
			{
				Id: &spec.BlockID{
					Number: blocks[0].number,
				},
				BodyMessage: &spec.BlockBodiesResponse_Classes{
					Classes: &spec.Classes{
						Domain: 0,
						Classes: []*spec.Class{
							{
								CompiledHash: core2p2p.AdaptHash(blocks[0].stateDiff.DeclaredV0Classes[0]),
								Definition:   []byte(cairo0Program),
							},
							{
								CompiledHash: core2p2p.AdaptHash(cairo1Class.Hash()),
								Definition:   []byte(cairo1Program),
							},
						},
					},
				},
			},
			{
				Id: &spec.BlockID{
					Number: blocks[0].number,
				},
				BodyMessage: &spec.BlockBodiesResponse_Fin{},
			},
			{
				Id:          nil,
				BodyMessage: &spec.BlockBodiesResponse_Fin{},
			},
		}

		var count int
		for body, valid := res(); valid; body, valid = res() {
			if count == 0 {
				diff := body.BodyMessage.(*spec.BlockBodiesResponse_Diff).Diff.ContractDiffs
				sortContractDiff(diff)

				expectedDiff := expectedMessages[count].BodyMessage.(*spec.BlockBodiesResponse_Diff).Diff.ContractDiffs
				sortContractDiff(expectedDiff)
			}

			if !assert.True(t, proto.Equal(expectedMessages[count], body), "iteration %d, type %T", count, body.BodyMessage) {
				spew.Dump(body.BodyMessage)
			}
			count++
		}
		require.Equal(t, len(expectedMessages), count)
	})

	t.Run("get receipts", func(t *testing.T) {
		res, cErr := client.RequestReceipts(testCtx, &spec.ReceiptsRequest{})
		require.NoError(t, cErr)

		count := uint64(0)
		for receipt, valid := res(); valid; receipt, valid = res() {
			assert.Equal(t, count, receipt.Id.Number)
			count++
		}
		require.Equal(t, uint64(4), count)
	})

	t.Run("get txns", func(t *testing.T) {
		blocks := []*core.Block{
			{
				Header: &core.Header{
					Number: 0,
				},
				Transactions: []core.Transaction{
					fillFelts(t, &core.DeployTransaction{
						ConstructorCallData: feltSlice(3),
					}),
					fillFelts(t, &core.L1HandlerTransaction{
						CallData: feltSlice(2),
						Version:  txVersion(1),
					}),
				},
			},
			{
				Header: &core.Header{
					Number: 1,
				},
				Transactions: []core.Transaction{
					fillFelts(t, &core.DeployAccountTransaction{
						DeployTransaction: core.DeployTransaction{
							ConstructorCallData: feltSlice(3),
							Version:             txVersion(1),
						},
						TransactionSignature: feltSlice(2),
					}),
				},
			},
			{
				Header: &core.Header{
					Number: 2,
				},
				Transactions: []core.Transaction{
					fillFelts(t, &core.DeclareTransaction{
						TransactionSignature: feltSlice(2),
						Version:              txVersion(0),
					}),
					fillFelts(t, &core.DeclareTransaction{
						TransactionSignature: feltSlice(2),
						Version:              txVersion(1),
					}),
				},
			},
			{
				Header: &core.Header{
					Number: 3,
				},
				Transactions: []core.Transaction{
					fillFelts(t, &core.InvokeTransaction{
						CallData:             feltSlice(3),
						TransactionSignature: feltSlice(2),
						Version:              txVersion(0),
					}),
					fillFelts(t, &core.InvokeTransaction{
						CallData:             feltSlice(4),
						TransactionSignature: feltSlice(2),
						Version:              txVersion(1),
					}),
				},
			},
		}
		numOfBlocks := uint64(len(blocks))

		for _, block := range blocks {
			mockReader.EXPECT().BlockByNumber(block.Number).Return(block, nil)
		}

		res, cErr := client.RequestTransactions(testCtx, &spec.TransactionsRequest{
			Iteration: &spec.Iteration{
				Start: &spec.Iteration_BlockNumber{
					BlockNumber: blocks[0].Number,
				},
				Direction: spec.Iteration_Forward,
				Limit:     numOfBlocks,
				Step:      1,
			},
		})
		require.NoError(t, cErr)

		var count uint64
		for txn, valid := res(); valid; txn, valid = res() {
			if count == numOfBlocks {
				assert.NotNil(t, txn.GetFin())
				break
			}

			assert.Equal(t, count, txn.Id.Number)

			expectedTx := mapToExpectedTransactions(blocks[count])
			assert.True(t, proto.Equal(expectedTx, txn.GetTransactions()))
			count++
		}
		require.Equal(t, numOfBlocks, count)
	})

	t.Run("get events", func(t *testing.T) {
		eventsPerBlock := [][]*core.Event{
			{}, // block with no events
			{
				{
					From: randFelt(t),
					Data: feltSlice(1),
					Keys: feltSlice(1),
				},
			},
			{
				{
					From: randFelt(t),
					Data: feltSlice(2),
					Keys: feltSlice(2),
				},
				{
					From: randFelt(t),
					Data: feltSlice(3),
					Keys: feltSlice(3),
				},
			},
		}
		for blockNumber, events := range eventsPerBlock {
			blockNumber := uint64(blockNumber)
			mockReader.EXPECT().BlockByNumber(blockNumber).Return(&core.Block{
				Header: &core.Header{
					Number: blockNumber,
				},
				Receipts: []*core.TransactionReceipt{
					{Events: events},
				},
			}, nil)
		}

		numOfBlocks := uint64(len(eventsPerBlock))
		res, cErr := client.RequestEvents(testCtx, &spec.EventsRequest{
			Iteration: &spec.Iteration{
				Start: &spec.Iteration_BlockNumber{
					BlockNumber: 0,
				},
				Direction: spec.Iteration_Forward,
				Limit:     numOfBlocks,
				Step:      1,
			},
		})
		require.NoError(t, cErr)

		var count uint64
		for evnt, valid := res(); valid; evnt, valid = res() {
			if count == numOfBlocks {
				assert.True(t, proto.Equal(&spec.Fin{}, evnt.GetFin()))
				count++
				break
			}

			assert.Equal(t, count, evnt.Id.Number)

			passedEvents := eventsPerBlock[int(count)]
			expectedEventsResponse := &spec.EventsResponse_Events{
				Events: &spec.Events{
					Items: utils.Map(passedEvents, func(e *core.Event) *spec.Event {
						adaptFelt := core2p2p.AdaptFelt
						return &spec.Event{
							FromAddress: adaptFelt(e.From),
							Keys:        utils.Map(e.Keys, adaptFelt),
							Data:        utils.Map(e.Data, adaptFelt),
						}
					}),
				},
			}

			assert.True(t, proto.Equal(expectedEventsResponse.Events, evnt.GetEvents()))
			count++
		}
		expectedCount := numOfBlocks + 1 // numOfBlocks messages with blocks + 1 fin message
		require.Equal(t, expectedCount, count)
	})
}

func mapToExpectedTransactions(block *core.Block) *spec.Transactions {
	return &spec.Transactions{
		Items: utils.Map(block.Transactions, mapToExpectedTransaction),
	}
}

func mapToExpectedTransaction(tx core.Transaction) *spec.Transaction {
	adaptHash := core2p2p.AdaptHash
	adaptFelt := core2p2p.AdaptFelt
	adaptAddress := core2p2p.AdaptAddress

	var resp spec.Transaction
	switch v := tx.(type) {
	case *core.DeployTransaction:
		resp.Txn = &spec.Transaction_Deploy_{
			Deploy: &spec.Transaction_Deploy{
				ClassHash:   adaptHash(v.ClassHash),
				AddressSalt: adaptFelt(v.ContractAddressSalt),
				Calldata:    utils.Map(v.ConstructorCallData, adaptFelt),
			},
		}
	case *core.L1HandlerTransaction:
		resp.Txn = &spec.Transaction_L1Handler{
			L1Handler: &spec.Transaction_L1HandlerV1{
				Nonce:              adaptFelt(v.Nonce),
				Address:            adaptAddress(v.ContractAddress),
				EntryPointSelector: adaptFelt(v.EntryPointSelector),
				Calldata:           utils.Map(v.CallData, adaptFelt),
			},
		}
	case *core.DeployAccountTransaction:
		resp.Txn = &spec.Transaction_DeployAccountV1_{
			DeployAccountV1: &spec.Transaction_DeployAccountV1{
				MaxFee:      adaptFelt(v.MaxFee),
				Signature:   core2p2p.AdaptAccountSignature(v.TransactionSignature),
				ClassHash:   adaptHash(v.ClassHash),
				Nonce:       adaptFelt(v.Nonce),
				AddressSalt: adaptFelt(v.ContractAddressSalt),
				Calldata:    utils.Map(v.ConstructorCallData, adaptFelt),
			},
		}
	case *core.DeclareTransaction:
		if v.Version.Is(0) {
			resp.Txn = &spec.Transaction_DeclareV0_{
				DeclareV0: &spec.Transaction_DeclareV0{
					Sender:    adaptAddress(v.SenderAddress),
					MaxFee:    adaptFelt(v.MaxFee),
					Signature: core2p2p.AdaptAccountSignature(v.Signature()),
					ClassHash: adaptHash(v.ClassHash),
				},
			}
		} else if v.Version.Is(1) {
			resp.Txn = &spec.Transaction_DeclareV1_{
				DeclareV1: &spec.Transaction_DeclareV1{
					Sender:    adaptAddress(v.SenderAddress),
					MaxFee:    adaptFelt(v.MaxFee),
					Signature: core2p2p.AdaptAccountSignature(v.Signature()),
					ClassHash: adaptHash(v.ClassHash),
					Nonce:     adaptFelt(v.Nonce),
				},
			}
		}
	case *core.InvokeTransaction:
		if v.Version.Is(0) {
			resp.Txn = &spec.Transaction_InvokeV0_{
				InvokeV0: &spec.Transaction_InvokeV0{
					MaxFee:             adaptFelt(v.MaxFee),
					Signature:          core2p2p.AdaptAccountSignature(v.Signature()),
					Address:            adaptAddress(v.ContractAddress),
					EntryPointSelector: adaptFelt(v.EntryPointSelector),
					Calldata:           utils.Map(v.CallData, adaptFelt),
				},
			}
		} else if v.Version.Is(1) {
			resp.Txn = &spec.Transaction_InvokeV1_{
				InvokeV1: &spec.Transaction_InvokeV1{
					Sender:    adaptAddress(v.SenderAddress),
					MaxFee:    adaptFelt(v.MaxFee),
					Signature: core2p2p.AdaptAccountSignature(v.Signature()),
					Calldata:  utils.Map(v.CallData, adaptFelt),
				},
			}
		}
	}
	return &resp
}

func txVersion(v uint64) *core.TransactionVersion {
	var f felt.Felt
	f.SetUint64(v)

	txV := core.TransactionVersion(f)
	return &txV
}

func feltSlice(n int) []*felt.Felt {
	return make([]*felt.Felt, n)
}

func randFelt(t *testing.T) *felt.Felt {
	t.Helper()

	f, err := new(felt.Felt).SetRandom()
	require.NoError(t, err)

	return f
}

func fillFelts[T any](t *testing.T, i T) T {
	v := reflect.ValueOf(i)
	if v.Kind() == reflect.Ptr && !v.IsNil() {
		v = v.Elem()
	}
	typ := v.Type()

	const feltTypeStr = "*felt.Felt"

	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		ftyp := typ.Field(i).Type // Get the type of the current field

		// Skip unexported fields
		if !f.CanSet() {
			continue
		}

		switch f.Kind() {
		case reflect.Ptr:
			// Check if the type is Felt
			if ftyp.String() == feltTypeStr {
				f.Set(reflect.ValueOf(randFelt(t)))
			} else if f.IsNil() {
				// Initialise the pointer if it's nil
				f.Set(reflect.New(ftyp.Elem()))
			}

			if f.Elem().Kind() == reflect.Struct {
				// Recursive call for nested structs
				fillFelts(t, f.Interface())
			}
		case reflect.Slice:
			// For slices, loop and populate
			for j := 0; j < f.Len(); j++ {
				elem := f.Index(j)
				if elem.Type().String() == feltTypeStr {
					elem.Set(reflect.ValueOf(randFelt(t)))
				}
			}
		case reflect.Struct:
			// Recursive call for nested structs
			fillFelts(t, f.Addr().Interface())
		}
	}

	return i
}

func sortContractDiff(diff []*spec.StateDiff_ContractDiff) {
	sort.Slice(diff, func(i, j int) bool {
		iAddress := diff[i].Address
		jAddress := diff[j].Address
		return bytes.Compare(iAddress.Elements, jAddress.Elements) < 0
	})
}
