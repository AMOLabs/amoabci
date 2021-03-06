package tx

import (
	"bytes"
	"encoding/json"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"

	"github.com/amolabs/amoabci/amo/code"
	"github.com/amolabs/amoabci/amo/store"
	"github.com/amolabs/amoabci/amo/types"
)

type TransferParam struct {
	// TODO: change to human-readable ascii string
	UDC    uint32           `json:"udc,omitempty"`
	To     crypto.Address   `json:"to"`
	Amount types.Currency   `json:"amount,omitempty"`
}

func parseTransferParam(raw []byte) (TransferParam, error) {
	var param TransferParam
	err := json.Unmarshal(raw, &param)
	return param, err
}

type TxTransfer struct {
	TxBase
	Param TransferParam `json:"-"`
}

var _ Tx = &TxTransfer{}

func (t *TxTransfer) Check() (uint32, string) {
	txParam, err := parseTransferParam(t.getPayload())
	if err != nil {
		return code.TxCodeBadParam, err.Error()
	}

	if len(txParam.To) != crypto.AddressSize {
		return code.TxCodeBadParam, "wrong recipient address size"
	}
	if bytes.Equal(t.GetSender(), txParam.To) {
		return code.TxCodeSelfTransaction, "tried to transfer to self"
	}
	return code.TxCodeOK, "ok"
}

func (t *TxTransfer) Execute(store *store.Store) (uint32, string, []abci.Event) {
	txParam, err := parseTransferParam(t.getPayload())
	if err != nil {
		return code.TxCodeBadParam, err.Error(), nil
	}

	if txParam.Amount.GreaterThan(zero) {
		return t.TransferCoin(store, txParam)
	} else {
		return code.TxCodeInvalidAmount, "invalid amount", nil
	}
}

func (t *TxTransfer) TransferCoin(store *store.Store, txParam TransferParam) (uint32, string, []abci.Event) {
	udc := txParam.UDC
	udcLock := store.GetUDCLock(udc, t.GetSender(), false)
	fromBalance := store.GetUDCBalance(udc, t.GetSender(), false)
	required := udcLock
	required.Add(&txParam.Amount)
	if fromBalance.LessThan(required) {
		return code.TxCodeNotEnoughBalance, "not enough balance", nil
	}
	toBalance := store.GetUDCBalance(txParam.UDC, txParam.To, false)
	fromBalance.Sub(&txParam.Amount)
	toBalance.Add(&txParam.Amount)
	store.SetUDCBalance(txParam.UDC, t.GetSender(), fromBalance)
	store.SetUDCBalance(txParam.UDC, txParam.To, toBalance)
	return code.TxCodeOK, "ok", nil
}
