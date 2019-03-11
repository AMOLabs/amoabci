package rpc

import (
	"encoding/json"
	"github.com/amolabs/amoabci/amo/operation"

	cmn "github.com/amolabs/tendermint-amo/libs/common"
	"github.com/amolabs/tendermint-amo/rpc/client"
	ctypes "github.com/amolabs/tendermint-amo/rpc/core/types"
	"github.com/amolabs/tendermint-amo/types"
)

var (
	rpcRemote     = "tcp://0.0.0.0:26657"
	rpcWsEndpoint = "/websocket"
)

// MakeMessage handles making tx message
func MakeMessage(t string, payload interface{}) types.Tx {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	msg := operation.Message{
		Command: t,
		Payload: raw,
	}
	tx, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}
	return tx
}

// RPCBroadcastTxCommit handles sending transactions
func RPCBroadcastTxCommit(tx types.Tx) (*ctypes.ResultBroadcastTxCommit, error) {
	cli := client.NewHTTP(rpcRemote, rpcWsEndpoint)
	return cli.BroadcastTxCommit(tx)
}

// RPCABCIQuery handles querying
func RPCABCIQuery(path string, data cmn.HexBytes) (*ctypes.ResultABCIQuery, error) {
	cli := client.NewHTTP(rpcRemote, rpcWsEndpoint)
	return cli.ABCIQuery(path, data)
}

// RPCStatus handle querying the status
func RPCStatus() (*ctypes.ResultStatus, error) {
	cli := client.NewHTTP(rpcRemote, rpcWsEndpoint)
	return cli.Status()
}