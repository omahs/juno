package prover

import (
	"testing"

	"github.com/NethermindEth/juno/core"
	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/db/pebble"
	"github.com/NethermindEth/juno/utils"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	testDB := pebble.NewMemTest(t)
	txn, err := testDB.NewTransaction(false)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, txn.Discard())
	})
	state := core.NewState(txn)

	t.Run("todo", func(t *testing.T) {
		err := New(nil).CairoRunnerRun(
			&BlockInfo{
				Header: &core.Header{
					Timestamp:        1666877926,
					SequencerAddress: utils.HexToFelt(t, "0x46a89ae102987331d369645031b49c27738ed096f2789c24449966da4c6de6b"),
					GasPrice:         &felt.Zero,
					GasPriceSTRK:     &felt.Zero,
				}},
			false,
			&utils.Sepolia,
			state,
		)
		require.Nil(t, err)
	})

}
