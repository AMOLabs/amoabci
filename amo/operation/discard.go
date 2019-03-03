package operation

import (
	"bytes"
	"github.com/amolabs/amoabci/amo/code"
	"github.com/amolabs/amoabci/amo/db"
	"github.com/amolabs/tendermint-amo/crypto"
	cmn "github.com/amolabs/tendermint-amo/libs/common"
)

var _ Operation = Discard{}

type Discard struct {
	Target cmn.HexBytes `json:"target"`
}

func (o Discard) Check(store *db.Store, signer crypto.Address) uint32 {
	parcel := store.GetParcel(o.Target)
	if parcel == nil {
		return code.TxCodeTargetNotExists
	}
	if !bytes.Equal(parcel.Owner, signer) {
		return code.TxCodePermissionDenied
	}
	return code.TxCodeOK
}

func (o Discard) Execute(store *db.Store, signer crypto.Address) (uint32, []cmn.KVPair) {
	store.DeleteParcel(o.Target)
	tags := []cmn.KVPair{
		{Key: []byte("target"), Value: []byte(o.Target.String())},
	}
	return code.TxCodeOK, tags
}
