package blockbuilder_test

import (
	"context"
	"testing"
	"time"

	"github.com/NethermindEth/juno/blockbuilder"
	"github.com/NethermindEth/juno/blockchain"
	"github.com/NethermindEth/juno/db/pebble"
	"github.com/NethermindEth/juno/mempool"
	"github.com/NethermindEth/juno/utils"
	"github.com/NethermindEth/juno/vm"
	"github.com/stretchr/testify/require"
)

func TestBuilder(t *testing.T) {
	testDB := pebble.NewMemTest(t)
	network := utils.Goerli2
	log := utils.NewNopZapLogger()
	chain := blockchain.New(testDB, network, log)
	starknetVM := vm.New(log)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	require.NoError(t, blockbuilder.New(chain, starknetVM, mempool.New()).Run(ctx))
	cancel()
}
