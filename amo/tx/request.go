package tx

import (
	"bytes"
	"encoding/json"

	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"

	"github.com/amolabs/amoabci/amo/code"
	"github.com/amolabs/amoabci/amo/store"
	"github.com/amolabs/amoabci/amo/types"
	"github.com/amolabs/amoabci/crypto/p256"
)

type RequestParam struct {
	Target          tmbytes.HexBytes `json:"target"`
	Payment         types.Currency   `json:"payment"`
	RecipientPubKey tmbytes.HexBytes `json:"recipient_pubkey"`
	Dealer          crypto.Address   `json:"dealer,omitempty"`
	DealerFee       types.Currency   `json:"dealer_fee,omitempty"`
	Extra           json.RawMessage  `json:"extra,omitempty"`
}

func parseRequestParam(raw []byte) (RequestParam, error) {
	var param RequestParam
	err := json.Unmarshal(raw, &param)
	if err != nil {
		return param, err
	}
	return param, nil
}

type TxRequest struct {
	TxBase
	Param RequestParam `json:"-"`
}

var _ Tx = &TxRequest{}

func (t *TxRequest) Check() (uint32, string) {
	// TOOD: check format
	//txParam, err := parseRequestParam(t.Payload)
	txParam, err := parseRequestParam(t.getPayload())
	if err != nil {
		return code.TxCodeBadParam, err.Error()
	}

	if len(txParam.RecipientPubKey) != p256.PubKeyP256Size {
		return code.TxCodeBadParam, "improper recipient pubkey"
	}

	return code.TxCodeOK, "ok"
}

func (t *TxRequest) Execute(store *store.Store) (uint32, string, []abci.Event) {
	txParam, err := parseRequestParam(t.getPayload())
	if err != nil {
		return code.TxCodeBadParam, err.Error(), nil
	}

	parcel := store.GetParcel(txParam.Target, false)
	if parcel == nil {
		return code.TxCodeParcelNotFound, "parcel not found", nil
	}

	if bytes.Equal(parcel.Owner, t.GetSender()) {
		// add new code for this
		return code.TxCodeSelfTransaction, "requesting owned parcel", nil
	}

	usage := store.GetUsage(t.GetSender(), txParam.Target, false)
	if usage != nil {
		return code.TxCodeAlreadyGranted, "parcel already granted", nil
	}

	request := store.GetRequest(t.GetSender(), txParam.Target, false)
	if request != nil {
		return code.TxCodeAlreadyRequested, "parcel already requested", nil
	}

	var recipientPubKey p256.PubKeyP256
	copy(recipientPubKey[:], txParam.RecipientPubKey)

	if len(txParam.Dealer) == 0 {
		txParam.DealerFee.Set(0)
	} else if len(txParam.Dealer) != crypto.AddressSize {
		return code.TxCodeBadParam, "invalid dealer address", nil
	}

	balance := store.GetBalance(t.GetSender(), false)
	wanted, err := txParam.Payment.Clone()
	if err != nil {
		return code.TxCodeInvalidAmount, err.Error(), nil
	}
	wanted.Add(&txParam.DealerFee)
	if balance.LessThan(wanted) {
		return code.TxCodeNotEnoughBalance, "not enough balance", nil
	}

	store.SetRequest(t.GetSender(), txParam.Target, &types.Request{
		Payment:         txParam.Payment,
		RecipientPubKey: recipientPubKey,
		Dealer:          txParam.Dealer,
		DealerFee:       txParam.DealerFee,
		Extra: types.Extra{
			Register: parcel.Extra.Register,
			Request:  txParam.Extra,
		},
	})

	balance.Sub(wanted)
	store.SetBalance(t.GetSender(), balance)

	return code.TxCodeOK, "ok", []abci.Event{}
}
