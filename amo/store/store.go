package store

import (
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/db"
	"github.com/tendermint/tendermint/types"

	"github.com/amolabs/amoabci/amo/encoding/binary"
	atypes "github.com/amolabs/amoabci/amo/types"
)

var (
	prefixBalance = []byte("balance:")
	prefixParcel  = []byte("parcel:")
	prefixRequest = []byte("request:")
	prefixUsage   = []byte("usage:")
)

type Store struct {
	dbm db.DB
}

func getGoLevelDB(name, dir string) *db.GoLevelDB {
	leveldb, err := db.NewGoLevelDB(name, dir)
	if err != nil {
		panic(err)
	}
	return leveldb
}

func NewStore(bDB db.DB) *Store {
	return &Store{dbm: bDB}
}

// Balance store
func getBalanceKey(addr types.Address) []byte {
	return append(prefixBalance, addr.Bytes()...)
}

func (s Store) Purge() error {
	var itr db.Iterator = s.dbm.Iterator([]byte{}, []byte(nil))
	defer itr.Close()

	// TODO: cannot guarantee in multi-thread environment
	// need some sync mechanism
	for ; itr.Valid(); itr.Next() {
		k := itr.Key()
		// XXX: not sure if this will confuse the iterator
		s.dbm.Delete(k)
	}

	// TODO: need some method like s.dbm.Size() to check if the DB has been
	// really emptied.

	return nil
}

func (s Store) SetBalance(addr types.Address, balance atypes.Currency) {
	b, _ := balance.Serialize()
	s.dbm.Set(getBalanceKey(addr), b)
}

func (s Store) GetBalance(addr types.Address) atypes.Currency {
	var c atypes.Currency
	balance := s.dbm.Get(getBalanceKey(addr))
	if len(balance) == 0 {
		return 0
	}
	err := binary.Deserialize(balance, &c)
	if err != nil {
		panic(err)
	}
	return c
}

// Parcel store
func getParcelKey(parcelID []byte) []byte {
	return append(prefixParcel, parcelID...)
}

func (s Store) SetParcel(parcelID []byte, value *atypes.ParcelValue) {
	b, err := value.Serialize()
	if err != nil {
		panic(err)
	}
	s.dbm.Set(getParcelKey(parcelID), b)
}

func (s Store) GetParcel(parcelID []byte) *atypes.ParcelValue {
	b := s.dbm.Get(getParcelKey(parcelID))
	if len(b) == 0 {
		return nil
	}
	var parcel atypes.ParcelValue
	err := binary.Deserialize(b, &parcel)
	if err != nil {
		panic(err)
	}
	return &parcel
}

func (s Store) DeleteParcel(parcelID []byte) {
	s.dbm.DeleteSync(getParcelKey(parcelID))
}

// Request store
func getRequestKey(buyer crypto.Address, parcelID []byte) []byte {
	return append(prefixRequest, append(append(buyer, ':'), parcelID...)...)
}

func (s Store) SetRequest(buyer crypto.Address, parcelID []byte, value *atypes.RequestValue) {
	b, err := value.Serialize()
	if err != nil {
		panic(err)
	}
	s.dbm.Set(getRequestKey(buyer, parcelID), b)
}

func (s Store) GetRequest(buyer crypto.Address, parcelID []byte) *atypes.RequestValue {
	b := s.dbm.Get(getRequestKey(buyer, parcelID))
	if len(b) == 0 {
		return nil
	}
	var request atypes.RequestValue
	err := binary.Deserialize(b, &request)
	if err != nil {
		panic(err)
	}
	return &request
}

func (s Store) DeleteRequest(buyer crypto.Address, parcelID []byte) {
	s.dbm.DeleteSync(getRequestKey(buyer, parcelID))
}

// Usage store
func getUsageKey(buyer crypto.Address, parcelID []byte) []byte {
	return append(prefixUsage, append(append(buyer, ':'), parcelID...)...)
}

func (s Store) SetUsage(buyer crypto.Address, parcelID []byte, value *atypes.UsageValue) {
	b, err := value.Serialize()
	if err != nil {
		panic(err)
	}
	s.dbm.Set(getUsageKey(buyer, parcelID), b)
}

func (s Store) GetUsage(buyer crypto.Address, parcelID []byte) *atypes.UsageValue {
	b := s.dbm.Get(getUsageKey(buyer, parcelID))
	if len(b) == 0 {
		return nil
	}
	var usage atypes.UsageValue
	err := binary.Deserialize(b, &usage)
	if err != nil {
		panic(err)
	}
	return &usage
}

func (s Store) DeleteUsage(buyer crypto.Address, parcelID []byte) {
	s.dbm.DeleteSync(getUsageKey(buyer, parcelID))
}
