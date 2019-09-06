package tx

import (
	"bytes"
	"encoding/json"

	"github.com/tendermint/tendermint/crypto"
	tm "github.com/tendermint/tendermint/libs/common"

	"github.com/amolabs/amoabci/amo/code"
	"github.com/amolabs/amoabci/amo/store"
	"github.com/amolabs/amoabci/amo/types"
)

type GrantParam struct {
	Target  tm.HexBytes    `json:"target"`
	Grantee crypto.Address `json:"grantee"`
	Custody tm.HexBytes    `json:"custody"`
}

func parseGrantParam(raw []byte) (GrantParam, error) {
	var param GrantParam
	err := json.Unmarshal(raw, &param)
	if err != nil {
		return param, err
	}
	return param, nil
}

func CheckGrant(t Tx) (uint32, string) {
	txParam, err := parseGrantParam(t.Payload)
	if err != nil {
		return code.TxCodeBadParam, err.Error()
	}

	// TODO: check format

	if len(txParam.Grantee) != crypto.AddressSize {
		return code.TxCodeBadParam, "wrong grantee address size"
	}

	return code.TxCodeOK, "ok"
}

func ExecuteGrant(t Tx, store *store.Store) (uint32, string, []tm.KVPair) {
	txParam, err := parseGrantParam(t.Payload)
	if err != nil {
		return code.TxCodeBadParam, err.Error(), nil
	}

	parcel := store.GetParcel(txParam.Target)
	if parcel == nil {
		return code.TxCodeParcelNotFound, "parcel not found", nil
	}
	if !bytes.Equal(parcel.Owner, t.Sender) {
		return code.TxCodePermissionDenied, "parcel not owned", nil
	}
	if store.GetUsage(txParam.Grantee, txParam.Target) != nil {
		return code.TxCodeAlreadyGranted, "parcel already granted", nil
	}
	request := store.GetRequest(txParam.Grantee, txParam.Target)
	if request == nil {
		return code.TxCodeRequestNotFound, "request not found", nil
	}

	store.DeleteRequest(txParam.Grantee, txParam.Target)
	balance := store.GetBalance(t.Sender)
	balance.Add(&request.Payment)
	store.SetBalance(t.Sender, balance)
	usage := types.UsageValue{
		Custody: txParam.Custody,
	}
	store.SetUsage(txParam.Grantee, txParam.Target, &usage)
	tags := []tm.KVPair{
		{Key: []byte("parcel.id"), Value: []byte(txParam.Target.String())},
	}
	return code.TxCodeOK, "ok", tags
}
