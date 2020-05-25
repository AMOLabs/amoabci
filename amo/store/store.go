package store

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/jmhodges/levigo"
	"github.com/tendermint/iavl"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/libs/log"
	tm "github.com/tendermint/tendermint/types"
	tmdb "github.com/tendermint/tm-db"

	"github.com/amolabs/amoabci/amo/code"
	"github.com/amolabs/amoabci/amo/types"
)

const (
	// division by 2 is for safeguarding. tendermint code.GetError(is not so safe.
	MaxTotalVotingPower = tm.MaxTotalVotingPower / 2

	merkleTreeCacheSize = 10000
)

var (
	prefixBalance  = []byte("balance:")
	prefixStake    = []byte("stake:")
	prefixDraft    = []byte("draft:")
	prefixVote     = []byte("vote:")
	prefixDelegate = []byte("delegate:")
	prefixParcel   = []byte("parcel:")
	prefixRequest  = []byte("request:")
	prefixUsage    = []byte("usage:")

	prefixIndexDelegator = []byte("delegator")
	prefixIndexValidator = []byte("validator")
	prefixIndexEffStake  = []byte("effstake")
)

type Store struct {
	logger log.Logger

	// merkle tree for blockchain state
	merkleDB      tmdb.DB
	merkleTree    *iavl.MutableTree
	merkleVersion int64

	indexDB tmdb.DB
	// search index for delegators:
	// XXX: a delegatee can have multiple delegators
	// key: delegatee address || delegator address
	// value: nil
	indexDelegator tmdb.DB
	// search index for validator:
	// key: validator address
	// value: holder address
	indexValidator tmdb.DB
	// ordered cache of effective stakes:
	// key: effective stake (32 bytes) || stake holder address
	// value: nil
	indexEffStake tmdb.DB

	// search index for block-first delivered txs
	// key: block height
	// value: hash of txs
	indexBlockTx tmdb.DB
	// search index for tx hash-first delivered txs
	// key: tx hash
	// value: block height
	indexTxBlock tmdb.DB

	incentiveDB tmdb.DB
	// search incentive for block height-first:
	// key: block height || stake holder address
	// value: incentive amount
	incentiveHeight tmdb.DB
	// search incentive for address-first:
	// key: stake holder addres || block height
	// value: incentive amount
	incentiveAddress tmdb.DB
	// search penalty for block height-first:
	// key: block height || stake holder address
	// value: penalty amount
	penaltyHeight tmdb.DB
	// search penalty for address-first:
	// key: stake holder addres || block height
	// value: penalty amount
	penaltyAddress tmdb.DB

	// lazinessCounter database
	lazinessCounterDB tmdb.DB
}

func NewStore(logger log.Logger, merkleDB, indexDB, incentiveDB, lazinessCounterDB tmdb.DB) (*Store, error) {
	// normal noprune
	//mt, err := iavl.NewMutableTree(merkleDB, merkleTreeCacheSize)
	// with prune
	memDB := tmdb.NewMemDB()
	mt, err := iavl.NewMutableTreeWithOpts(merkleDB, memDB,
		merkleTreeCacheSize, iavl.PruningOptions(1000, 1))

	if err != nil {
		return nil, err
	}

	return &Store{
		logger: logger,

		merkleDB:      merkleDB,
		merkleTree:    mt,
		merkleVersion: 0,

		indexDB:        indexDB,
		indexDelegator: tmdb.NewPrefixDB(indexDB, prefixIndexDelegator),
		indexValidator: tmdb.NewPrefixDB(indexDB, prefixIndexValidator),
		indexEffStake:  tmdb.NewPrefixDB(indexDB, prefixIndexEffStake),
		indexBlockTx:   tmdb.NewPrefixDB(indexDB, prefixIndexBlockTx),
		indexTxBlock:   tmdb.NewPrefixDB(indexDB, prefixIndexTxBlock),

		incentiveDB:      incentiveDB,
		incentiveHeight:  tmdb.NewPrefixDB(incentiveDB, prefixIncentiveHeight),
		incentiveAddress: tmdb.NewPrefixDB(incentiveDB, prefixIncentiveAddress),
		penaltyHeight:    tmdb.NewPrefixDB(incentiveDB, prefixPenaltyHeight),
		penaltyAddress:   tmdb.NewPrefixDB(incentiveDB, prefixPenaltyAddress),

		lazinessCounterDB: lazinessCounterDB,
	}, nil
}

func (s Store) GetMerkleVersion() int64 {
	return s.merkleVersion
}

func (s Store) Purge() error {
	// merkleTree
	// delete all available tree versions
	v, err := s.merkleTree.LoadVersionForOverwriting(0)
	if err != nil {
		return err
	}
	if v != 0 {
		return errors.New("couldn't purge merkle tree")
	}
	err = purgeDB(s.merkleDB)
	if err != nil {
		return err
	}

	// indexDB
	err = purgeDB(s.indexDB)
	if err != nil {
		return err
	}

	// incentiveDB
	err = purgeDB(s.incentiveDB)
	if err != nil {
		return err
	}

	// lazinessCounterDB
	err = purgeDB(s.lazinessCounterDB)
	if err != nil {
		return err
	}

	s.Compact()

	s.merkleTree, err = iavl.NewMutableTree(s.merkleDB, merkleTreeCacheSize)
	if err != nil {
		return err
	}

	return nil
}

func purgeDB(db tmdb.DB) error {
	itr, err := db.Iterator(nil, nil)
	if err != nil {
		return err
	}
	b := db.NewBatch()
	for ; itr.Valid(); itr.Next() {
		k := itr.Key()
		// XXX: not sure if this will confuse the iterator
		b.Delete(k)
	}
	itr.Close()
	b.WriteSync()
	b.Close()
	return nil
}

// MERKLE TREE SCOPE
// set -> working tree node (ONLY)
// get(committed: true)  -> the latest saved tree node
// 	  (committed: false) -> working tree node

// MERKLE TREE WORKFLOW
// set 	: working tree
// save : working tree -> saved tree

// node(key, value) -> working tree

func (s Store) has(key []byte) bool {
	return s.merkleTree.Has(key)
}

func (s Store) set(key, value []byte) bool {
	return s.merkleTree.Set(key, value)
}

// { working tree || saved tree } -> node(key, value)
func (s Store) get(key []byte, committed bool) []byte {
	if !committed {
		_, value := s.merkleTree.Get(key)
		return value
	}

	_, value := s.merkleTree.GetVersioned(key, s.merkleVersion)
	return value
}

// working tree, delete node(key, value)
func (s Store) remove(key []byte) ([]byte, bool) {
	return s.merkleTree.Remove(key)
}

// working tree >> saved tree
func (s *Store) Save() ([]byte, int64, error) {
	hash, ver, err := s.merkleTree.SaveVersion()
	s.merkleVersion = ver
	return hash, ver, err
}

// Load the latest versioned tree from disk.
func (s *Store) Load() (vers int64, err error) {
	vers, err = s.merkleTree.Load()
	s.merkleVersion = vers
	return
}

func (s *Store) LoadVersion(version int64) (vers int64, err error) {
	vers, err = s.merkleTree.LoadVersionForOverwriting(version)
	s.merkleVersion = vers
	return
}

func (s Store) Root() []byte {
	// NOTES
	// Hash() : Hash returns the hash of the latest saved version of the tree,
	// as returned by SaveVersion. If no versions have been saved, Hash returns nil.
	//
	// WorkingHash() : WorkingHash returns the hash of the current working tree.

	return s.merkleTree.WorkingHash()
}

func (s Store) Verify(key []byte) (bool, error) {
	return true, nil
}

func (s Store) getImmutableTree(committed bool) (*iavl.ImmutableTree, error) {
	if !committed {
		return s.merkleTree.ImmutableTree, nil
	}

	imt, err := s.merkleTree.GetImmutable(s.merkleVersion)
	if err != nil {
		return nil, err
	}

	return imt, nil
}

// Balance store
func makeBalanceKey(addr tm.Address) []byte {
	return append(prefixBalance, addr.Bytes()...)
}

func (s Store) SetBalance(addr tm.Address, balance *types.Currency) error {
	balanceKey := makeBalanceKey(addr)

	if balance.LessThan(types.Zero) {
		return fmt.Errorf("unavailable amount: %s", balance.String())
	}

	// pre-process for setting zero balance, just remove corresponding key
	if s.has(balanceKey) && balance.Equals(types.Zero) {
		s.remove(balanceKey)
		return nil
	}

	b, err := json.Marshal(balance)
	if err != nil {
		return err
	}

	s.set(balanceKey, b)

	return nil
}

func (s Store) SetBalanceUint64(addr tm.Address, balance uint64) error {

	zero := uint64(0)
	balanceKey := makeBalanceKey(addr)

	// pre-process for setting zero balance, just remove corresponding key
	if s.has(balanceKey) && balance == zero {
		s.remove(balanceKey)
		return nil
	}

	b, err := json.Marshal(new(types.Currency).Set(balance))
	if err != nil {
		return err
	}

	s.set(balanceKey, b)

	return nil
}

func (s Store) GetBalance(addr tm.Address, committed bool) *types.Currency {
	c := types.Currency{}
	balance := s.get(makeBalanceKey(addr), committed)
	if len(balance) == 0 {
		return &c
	}
	err := json.Unmarshal(balance, &c)
	if err != nil {
		return &c
	}
	return &c
}

// Stake store
func makeStakeKey(holder []byte) []byte {
	return append(prefixStake, holder...)
}

func makeLockedStakeKey(holder []byte, height int64) []byte {
	hb := make([]byte, 8)
	binary.BigEndian.PutUint64(hb, uint64(height))
	dbKey := append(prefixStake, holder...)
	dbKey = append(dbKey, hb...)
	return dbKey
}

func splitLockedStakeKey(key []byte) (crypto.Address, int64) {
	if len(key) != len(prefixStake)+crypto.AddressSize+8 {
		return nil, 0
	}
	h := binary.BigEndian.Uint64(key[len(prefixStake)+crypto.AddressSize:])
	return key[len(prefixStake) : len(prefixStake)+crypto.AddressSize], int64(h)
}

func (s Store) checkValidatorMatch(holder crypto.Address, stake *types.Stake, committed bool) error {
	prevHolder := s.GetHolderByValidator(stake.Validator.Address(), committed)
	if prevHolder != nil && !bytes.Equal(prevHolder, holder) {
		return code.GetError(code.TxCodePermissionDenied)
	}
	prevStake := s.GetStake(holder, committed)
	if prevStake != nil &&
		!bytes.Equal(prevStake.Validator[:], stake.Validator[:]) {
		return code.GetError(code.TxCodeBadValidator)
	}
	return nil
}

func (s Store) checkStakeDeletion(holder crypto.Address, stake *types.Stake, height int64, committed bool) error {
	if stake.Amount.Sign() == 0 {
		whole := s.GetStake(holder, committed)
		if whole == nil {
			// something wrong. but harmless for now.
			return nil
		}
		var target *types.Stake
		if height == 0 {
			target = s.GetUnlockedStake(holder, committed)
		} else if height > 0 {
			target = s.GetLockedStake(holder, height, committed)
		} else { // height must not be negative
			return code.GetError(code.TxCodeUnknown)
		}
		whole.Amount.Sub(&target.Amount)
		if whole.Amount.Sign() == 0 {
			// whole stake for this holder goes to zero. need to check this is
			// allowed.

			// check if there is a delegate appointed to this stake
			ds := s.GetDelegatesByDelegatee(holder, committed)
			if len(ds) > 0 {
				return code.GetError(code.TxCodeDelegateExists)
			}

			// check if this is the last stake
			ts := s.GetTopStakes(2, nil, committed)
			if len(ts) == 1 {
				// requested 2 but got 1. it means this is the last validator.
				return code.GetError(code.TxCodeLastValidator)
			}
		}
	}

	return nil
}

func (s Store) SetUnlockedStake(holder crypto.Address, stake *types.Stake) error {
	b, err := json.Marshal(stake)
	if err != nil {
		return code.GetError(code.TxCodeBadParam)
	}

	// condition checks
	err = s.checkValidatorMatch(holder, stake, false)
	if err != nil {
		return err
	}
	err = s.checkStakeDeletion(holder, stake, 0, false)
	if err != nil {
		return err
	}

	// clean up
	es := s.GetEffStake(holder, false)
	if es != nil {
		before := makeEffStakeKey(s.GetEffStake(holder, false).Amount, holder)
		exist, err := s.indexEffStake.Has(before)
		if err != nil {
			s.logger.Error("Store", "SetUnlockedStake", err.Error())
			return code.GetError(code.TxCodeUnknown)
		}
		if exist {
			err := s.indexEffStake.Delete(before)
			if err != nil {
				s.logger.Error("Store", "SetUnlockedStake", err.Error())
				return code.GetError(code.TxCodeUnknown)
			}
		}
	}
	// update
	if stake.Amount.Sign() == 0 {
		s.remove(makeStakeKey(holder))
		err := s.indexValidator.Delete(stake.Validator.Address())
		if err != nil {
			s.logger.Error("Store", "SetUnlockedStake", err.Error())
			return code.GetError(code.TxCodeUnknown)
		}
	} else {
		s.set(makeStakeKey(holder), b)
		err := s.indexValidator.Set(stake.Validator.Address(), holder)
		if err != nil {
			s.logger.Error("Store", "SetUnlockedStake", err.Error())
			return code.GetError(code.TxCodeUnknown)
		}
		after := makeEffStakeKey(s.GetEffStake(holder, false).Amount, holder)
		err = s.indexEffStake.Set(after, nil)
		if err != nil {
			s.logger.Error("Store", "SetUnlockedStake", err.Error())
			return code.GetError(code.TxCodeUnknown)
		}
	}

	return nil
}

// SetLockedStake stores a stake locked at *height*. The stake's height is
// decremented each time when LoosenLockedStakes is called.
func (s Store) SetLockedStake(holder crypto.Address, stake *types.Stake, height int64) error {
	b, err := json.Marshal(stake)
	if err != nil {
		return code.GetError(code.TxCodeBadParam)
	}

	// condition checks
	err = s.checkValidatorMatch(holder, stake, false)
	if err != nil {
		return err
	}
	err = s.checkStakeDeletion(holder, stake, height, false)
	if err != nil {
		return err
	}

	// clean up
	es := s.GetEffStake(holder, false)
	if es != nil {
		before := makeEffStakeKey(s.GetEffStake(holder, false).Amount, holder)
		exist, err := s.indexEffStake.Has(before)
		if err != nil {
			s.logger.Error("Store", "SetLockedStake", err.Error())
			return code.GetError(code.TxCodeUnknown)
		}
		if exist {
			err := s.indexEffStake.Delete(before)
			if err != nil {
				s.logger.Error("Store", "SetLockedStake", err.Error())
				return code.GetError(code.TxCodeUnknown)
			}
		}
	}

	// update
	if stake.Amount.Sign() == 0 {
		s.remove(makeLockedStakeKey(holder, height))
		err := s.indexValidator.Delete(stake.Validator.Address())
		if err != nil {
			s.logger.Error("Store", "SetLockedStake", err.Error())
			return code.GetError(code.TxCodeUnknown)
		}
	} else {
		s.set(makeLockedStakeKey(holder, height), b)
		err := s.indexValidator.Set(stake.Validator.Address(), holder)
		if err != nil {
			s.logger.Error("Store", "SetLockedStake", err.Error())
			return code.GetError(code.TxCodeUnknown)
		}
		after := makeEffStakeKey(s.GetEffStake(holder, false).Amount, holder)
		err = s.indexEffStake.Set(after, nil)
		if err != nil {
			s.logger.Error("Store", "SetLockedStake", err.Error())
			return code.GetError(code.TxCodeUnknown)
		}
	}

	return nil
}

func (s Store) SlashStakes(holder crypto.Address, amount types.Currency, committed bool) {
	total := s.GetStake(holder, committed)
	if total == nil {
		return
	}

	unlockedStake := s.GetUnlockedStake(holder, committed)
	lockedStakes, heights := s.GetLockedStakesWithHeight(holder, committed)

	// slash until amount gets 0
	// slash unlockedStake first, then lockedStakes

	if unlockedStake != nil {
		switch amount.Cmp(&unlockedStake.Amount.Int) {
		case -1: // amount < unlockedStake.Amount
			unlockedStake.Amount.Sub(&amount)
			amount.Set(0)
		case 0: // amount == unlockedStake.Amount
			unlockedStake.Amount.Set(0)
			amount.Set(0)
		case 1: // amount > unlockedStake.Amount
			unlockedStake.Amount.Set(0)
			amount.Sub(&unlockedStake.Amount)
		}

		s.SetUnlockedStake(holder, unlockedStake)

		// check end of slash
		if amount.Equals(types.Zero) {
			return
		}
	}

	// from height(close to unlocked) to EOA
	for i := len(lockedStakes) - 1; i >= 0; i-- {
		lockedStake := lockedStakes[i]
		height := heights[i]

		switch amount.Cmp(&lockedStake.Amount.Int) {
		case -1: // amount < lockedStake.Amount
			lockedStake.Amount.Sub(&amount)
			amount.Set(0)
		case 0: // amount == lockedStake.Amount
			lockedStake.Amount.Set(0)
			amount.Set(0)
		case 1: // amount > lockedStake.Amount
			lockedStake.Amount.Set(0)
			amount.Sub(&lockedStake.Amount)
		}

		s.SetLockedStake(holder, lockedStake, height)

		if amount.Equals(types.Zero) {
			break
		}
	}
}

func (s Store) UnlockStakes(holder crypto.Address, height int64, committed bool) {
	start := makeLockedStakeKey(holder, 0)
	end := makeLockedStakeKey(holder, height)

	unlocked := s.GetUnlockedStake(holder, committed)

	imt, err := s.getImmutableTree(committed)
	if err != nil {
		return
	}

	imt.IterateRangeInclusive(start, end, true, func(key []byte, value []byte, version int64) bool {
		stake := new(types.Stake)
		err := json.Unmarshal(value, stake)
		if err != nil {
			// We cannot recover from this error.
			// Since this function returns nothing, just skip this stake.
			return false // same as 'continue'
		}
		s.remove(key)
		if unlocked == nil {
			unlocked = stake
		} else {
			unlocked.Amount.Add(&stake.Amount)
		}
		return false
	})
	s.SetUnlockedStake(holder, unlocked)
}

func (s Store) LoosenLockedStakes(committed bool) {
	imt, err := s.getImmutableTree(committed)
	if err != nil {
		return
	}

	imt.IterateRangeInclusive(prefixStake, nil, true, func(key []byte, value []byte, version int64) bool {
		if !bytes.HasPrefix(key, prefixStake) {
			return false
		}

		if len(key) == len(prefixStake)+crypto.AddressSize {
			// unlocked stake
			return false // continue
		}

		holder, height := splitLockedStakeKey(key)
		if holder == nil || height <= 0 {
			// db corruption detected. but we can do nothing here. just skip.
			return false // continue
		}

		stake := new(types.Stake)
		err := json.Unmarshal(value, stake)
		if err != nil {
			// We cannot recover from this error.
			// Since this function returns nothing, just skip this stake.
			return false // continue
		}

		s.remove(key)
		height--
		if height == 0 {
			unlocked := s.GetUnlockedStake(holder, committed)
			if unlocked == nil {
				unlocked = stake
			} else {
				unlocked.Amount.Add(&stake.Amount)
			}
			err := s.SetUnlockedStake(holder, unlocked)
			if err != nil {
				return false // continue
			}
		} else {
			err := s.SetLockedStake(holder, stake, height)
			if err != nil {
				return false // continue
			}
		}
		return false
	})
}

func makeEffStakeKey(amount types.Currency, holder crypto.Address) []byte {
	key := make([]byte, 32+20) // 256-bit integer + 20-byte address
	b := amount.Bytes()
	copy(key[32-len(b):], b)
	copy(key[32:], holder)
	return key
}

func (s Store) GetStake(holder crypto.Address, committed bool) *types.Stake {
	stake := s.GetUnlockedStake(holder, committed)

	stakes := s.GetLockedStakes(holder, committed)
	for _, item := range stakes {
		if stake == nil {
			stake = item
		} else {
			// check db integrity
			if !bytes.Equal(stake.Validator[:], item.Validator[:]) {
				return nil
			}
			stake.Amount.Add(&item.Amount)
		}
	}

	return stake
}

func (s Store) GetUnlockedStake(holder crypto.Address, committed bool) *types.Stake {
	b := s.get(makeStakeKey(holder), committed)
	if len(b) == 0 {
		return nil
	}
	var stake types.Stake
	err := json.Unmarshal(b, &stake)
	if err != nil {
		return nil
	}
	return &stake
}

func (s Store) GetLockedStake(holder crypto.Address, height int64, committed bool) *types.Stake {
	b := s.get(makeLockedStakeKey(holder, height), committed)
	if len(b) == 0 {
		return nil
	}
	var stake types.Stake
	err := json.Unmarshal(b, &stake)
	if err != nil {
		return nil
	}
	return &stake
}

func (s Store) GetLockedStakes(holder crypto.Address, committed bool) []*types.Stake {
	holderKey := makeStakeKey(holder)
	start := makeLockedStakeKey(holder, 0)

	var stakes []*types.Stake
	// XXX: This routine may be used to get all free and locked stakes for a
	// holder. But, let's differentiate getUnlockedStake() and
	// GetLockedStakes() for now.

	imt, err := s.getImmutableTree(committed)
	if err != nil {
		return nil
	}

	imt.IterateRangeInclusive(start, nil, false, func(key []byte, value []byte, version int64) bool {
		if !bytes.HasPrefix(key, holderKey) {
			return false
		}

		stake := new(types.Stake)
		err := json.Unmarshal(value, stake)
		if err != nil {
			// We cannot recover from this error
			return false
		}

		stakes = append(stakes, stake)

		return false
	})

	return stakes
}

func (s Store) GetLockedStakesWithHeight(holder crypto.Address, committed bool) ([]*types.Stake, []int64) {
	holderKey := makeStakeKey(holder)
	start := makeLockedStakeKey(holder, 0)

	var (
		stakes  []*types.Stake
		heights []int64
	)

	imt, err := s.getImmutableTree(committed)
	if err != nil {
		return nil, nil
	}

	imt.IterateRangeInclusive(start, nil, false, func(key []byte, value []byte, version int64) bool {
		if !bytes.HasPrefix(key, holderKey) {
			return false
		}

		stake := new(types.Stake)
		err := json.Unmarshal(value, stake)
		if err != nil {
			// We cannot recover from this error
			return false
		}

		height := binary.BigEndian.Uint64(key[len(prefixStake)+crypto.AddressSize:])

		stakes = append(stakes, stake)
		heights = append(heights, int64(height))

		return false
	})

	return stakes, heights
}
func (s Store) GetStakeByValidator(addr crypto.Address, committed bool) *types.Stake {
	holder := s.GetHolderByValidator(addr, committed)
	if holder == nil {
		return nil
	}
	return s.GetStake(holder, committed)
}

func (s Store) GetHolderByValidator(addr crypto.Address, committed bool) []byte {
	holder, err := s.indexValidator.Get(addr)
	if err != nil {
		s.logger.Error("Store", "GetHolderByValidator", err.Error())
		return nil
	}

	return holder
}

// Delegate store
func makeDelegateKey(holder []byte) []byte {
	return append(prefixDelegate, holder...)
}

// Update data on stateDB, indexDelegator, indexEffStake
func (s Store) SetDelegate(holder crypto.Address, delegate *types.Delegate) error {
	b, err := json.Marshal(delegate)
	if err != nil {
		return code.GetError(code.TxCodeBadParam)
	}
	// before state update
	es := s.GetEffStake(delegate.Delegatee, false)
	if es == nil {
		return code.GetError(code.TxCodeNoStake)
	}

	// make effStakeKey to find its corresponding value
	before := makeEffStakeKey(es.Amount, delegate.Delegatee)
	exist, err := s.indexEffStake.Has(before)
	if err != nil {
		s.logger.Error("Store", "SetDelegate", err.Error())
		return code.GetError(code.TxCodeUnknown)
	}
	if exist {
		err := s.indexEffStake.Delete(before)
		if err != nil {
			s.logger.Error("Store", "SetDelegate", err.Error())
			return code.GetError(code.TxCodeUnknown)
		}
	}

	// upadate
	if delegate.Amount.Sign() == 0 {
		s.remove(makeDelegateKey(holder))
		err := s.indexDelegator.Delete(append(delegate.Delegatee, holder...))
		if err != nil {
			s.logger.Error("Store", "SetDelegate", err.Error())
			return code.GetError(code.TxCodeUnknown)
		}
	} else {
		s.set(makeDelegateKey(holder), b)
		err := s.indexDelegator.Set(append(delegate.Delegatee, holder...), nil)
		if err != nil {
			s.logger.Error("Store", "SetDelegate", err.Error())
			return code.GetError(code.TxCodeUnknown)
		}
	}

	after := makeEffStakeKey(
		s.GetEffStake(delegate.Delegatee, false).Amount,
		delegate.Delegatee,
	)

	err = s.indexEffStake.Set(after, nil)
	if err != nil {
		s.logger.Error("Store", "SetDelegate", err.Error())
		return code.GetError(code.TxCodeUnknown)
	}

	return nil
}

func (s Store) GetDelegate(holder crypto.Address, committed bool) *types.Delegate {
	b := s.get(makeDelegateKey(holder), committed)
	if len(b) == 0 {
		return nil
	}
	var delegate types.Delegate
	err := json.Unmarshal(b, &delegate)
	if err != nil {
		return nil
	}
	return &delegate
}

func (s Store) GetDelegateEx(holder crypto.Address, committed bool) *types.DelegateEx {
	delegate := s.GetDelegate(holder, committed)
	if delegate == nil {
		return nil
	}
	return &types.DelegateEx{Delegator: holder, Delegate: delegate}
}

func (s Store) GetDelegatesByDelegatee(delegatee crypto.Address, committed bool) []*types.DelegateEx {
	itr, err := s.indexDelegator.Iterator(delegatee, nil)
	if err != nil {
		s.logger.Error("Store", "GetDelegatesByDelegatee", err.Error())
		return nil
	}
	defer itr.Close()

	var delegates []*types.DelegateEx
	for ; itr.Valid() && bytes.HasPrefix(itr.Key(), delegatee); itr.Next() {
		delegator := itr.Key()[len(delegatee):]
		delegateEx := s.GetDelegateEx(delegator, committed)
		if delegateEx == nil {
			continue
		}
		delegates = append(delegates, delegateEx)
	}
	return delegates
}

func (s Store) GetEffStake(delegatee crypto.Address, committed bool) *types.Stake {
	stake := s.GetStake(delegatee, committed)
	if stake == nil {
		return nil
	}
	for _, d := range s.GetDelegatesByDelegatee(delegatee, committed) {
		stake.Amount.Add(&d.Amount)
	}
	return stake
}

func (s Store) GetTopStakes(max uint64, peek crypto.Address, committed bool) []*types.Stake {
	var (
		stakes []*types.Stake
		cnt    uint64 = 0
	)
	itr, err := s.indexEffStake.ReverseIterator(nil, nil)
	if err != nil {
		s.logger.Error("Store", "GetTopStakes", err.Error())
		return nil
	}
	for ; itr.Valid(); itr.Next() {
		if cnt >= max {
			break
		}
		key := itr.Key()
		var amount types.Currency
		amount.SetBytes(key[:32])
		holder := key[32:]
		// peeking mode
		if len(peek) > 0 {
			if bytes.Equal(holder, peek) {
				stakes = append(stakes, s.GetStake(holder, committed))
				return stakes
			}
		} else {
			stake := s.GetStake(holder, committed)
			stake.Amount = amount
			stakes = append(stakes, stake)
		}
		cnt++
		// TODO: assert GetEffStake() gives the same result
	}

	return stakes
}

// Draft store
func makeDraftKey(draftID uint32) []byte {
	return append(prefixDraft, ConvIDFromUint(draftID)...)
}

func (s Store) SetDraft(draftID uint32, value *types.Draft) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	s.set(makeDraftKey(draftID), b)

	return nil
}

func (s Store) GetDraft(draftID uint32, committed bool) *types.Draft {
	b := s.get(makeDraftKey(draftID), committed)
	if len(b) == 0 {
		return nil
	}

	var draft types.Draft
	err := json.Unmarshal(b, &draft)
	if err != nil {
		return nil
	}

	return &draft
}

func (s Store) ProcessDraftVotes(
	latestDraftIDUint uint32,
	maxValidators uint64,
	quorumRate, passRate, refundRate float64,
	committed bool,
) {
	voteJustGotClosed := false
	applyDraftConfig := false

	// check if there is a draft in process first
	draft := s.GetDraft(latestDraftIDUint, committed)

	// ignore non-existing draft
	if draft == nil {
		return
	}

	// ignore already applied draft
	if draft.OpenCount == 0 && draft.CloseCount == 0 && draft.ApplyCount == 0 {
		return
	}

	// decrement draft's open, close, apply counts
	if draft.OpenCount > 0 && draft.CloseCount > 0 && draft.ApplyCount > 0 {
		draft.OpenCount -= int64(1)
	} else if draft.OpenCount == 0 && draft.CloseCount > 0 && draft.ApplyCount > 0 {
		draft.CloseCount -= int64(1)
		if draft.CloseCount == 0 {
			voteJustGotClosed = true
		}
	} else if draft.OpenCount == 0 && draft.CloseCount == 0 && draft.ApplyCount > 0 {
		draft.ApplyCount -= int64(1)
		if draft.ApplyCount == 0 {
			applyDraftConfig = true
		}
	}

	// if draft just gets closed, update draft's tally value and handle deposit
	if voteJustGotClosed {
		// calculate draft.TallyQuorum
		tes := new(types.Currency).Set(0)
		tss := s.GetTopStakes(maxValidators, nil, committed)
		for _, ts := range tss {
			holder := s.GetHolderByValidator(ts.Validator.Address(), committed)
			es := s.GetEffStake(holder, committed)
			tes.Add(&es.Amount)
		}

		// tallyQuorum = totalEffectiveStake * quorumRate
		tesf := new(big.Float).SetInt(&tes.Int)
		qrf := new(big.Float).SetFloat64(quorumRate)
		qf := tesf.Mul(tesf, qrf)

		tallyQuorum := new(types.Currency)
		qf.Int(&tallyQuorum.Int)

		draft.TallyQuorum = *tallyQuorum

		// calculate vote.Power, draft.TallyApprove and draft.TallyReject
		pes := s.GetEffStake(draft.Proposer, committed)
		draft.TallyApprove.Add(&pes.Amount)

		votes := s.GetVotes(latestDraftIDUint, committed)
		for _, vote := range votes {
			// if not included in top stakes, ignore and delete vote
			ts := s.GetTopStakes(maxValidators, vote.Voter, committed)
			if len(ts) == 0 {
				s.DeleteVote(latestDraftIDUint, vote.Voter)
				continue
			}

			es := s.GetEffStake(vote.Voter, committed)

			// update vote's tally fields
			if vote.Vote.Approve {
				draft.TallyApprove.Add(&es.Amount)
			} else {
				draft.TallyReject.Add(&es.Amount)
			}
		}

		// totalTally = draft.TallyApprove + draft.TallyReject
		totalTally := new(types.Currency).Set(0)
		totalTally.Add(&draft.TallyApprove)
		totalTally.Add(&draft.TallyReject)

		// refund = totalTally * refundRate
		tesf = new(big.Float).SetInt(&totalTally.Int)
		rrf := new(big.Float).SetFloat64(refundRate)
		rf := tesf.Mul(tesf, rrf)

		refund := new(types.Currency)
		rf.Int(&refund.Int)

		// if draft.TallyApprove > refund
		if draft.TallyApprove.GreaterThan(refund) {
			// return deposit to proposer
			balance := s.GetBalance(draft.Proposer, committed)
			balance.Add(&draft.Deposit)
			s.SetBalance(draft.Proposer, balance)
		} else {
			// distribute deposit to voters
			votes := s.GetVotes(latestDraftIDUint, committed)

			// distAmount = draft.Deposit / len(votes)
			df := new(big.Float).SetInt(&draft.Deposit.Int)
			nf := new(big.Float).SetUint64(uint64(len(votes)))
			daf := df.Quo(df, nf)

			distAmount := new(types.Currency)
			daf.Int(&distAmount.Int)

			for _, vote := range votes {
				balance := s.GetBalance(vote.Voter, committed)
				balance.Add(distAmount)
				s.SetBalance(vote.Voter, balance)
			}
		}
	}

	s.SetDraft(latestDraftIDUint, draft)

	if applyDraftConfig {
		// totalTally = draft.TallyApprove + draft.TallyReject
		totalTally := new(types.Currency).Set(0)
		totalTally.Add(&draft.TallyApprove)
		totalTally.Add(&draft.TallyReject)

		// if draft.TallyQuorum > totalTally, drop draft config
		if draft.TallyQuorum.GreaterThan(totalTally) {
			return
		}

		// pass = totalTally * passRate
		ttf := new(big.Float).SetInt(&totalTally.Int)
		prf := new(big.Float).SetFloat64(passRate)
		pf := ttf.Mul(ttf, prf)

		pass := new(types.Currency)
		pf.Int(&pass.Int)

		// if pass > draft.TallyApprove, drop draft config
		if pass.GreaterThan(&draft.TallyApprove) {
			return
		}

		b, err := json.Marshal(draft.Config)
		if err != nil {
			return
		}

		s.SetAppConfig(b)
	}
}

// Vote store
func makeVoteKey(draftID uint32, voter crypto.Address) []byte {
	return append(prefixVote, append(ConvIDFromUint(draftID), voter...)...)
}

func (s Store) SetVote(draftID uint32, voter crypto.Address, value *types.Vote) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	s.set(makeVoteKey(draftID, voter), b)

	return nil
}

func (s Store) GetVote(draftID uint32, voter crypto.Address, committed bool) *types.Vote {
	b := s.get(makeVoteKey(draftID, voter), committed)
	if len(b) == 0 {
		return nil
	}

	var vote types.Vote
	err := json.Unmarshal(b, &vote)
	if err != nil {
		return nil
	}

	return &vote
}

func (s Store) GetVotes(draftID uint32, committed bool) []*types.VoteInfo {
	voteKey := makeVoteKey(draftID, []byte{})

	var voteInfo []*types.VoteInfo

	imt, err := s.getImmutableTree(committed)
	if err != nil {
		return nil
	}

	imt.IterateRangeInclusive(voteKey, nil, false, func(key []byte, value []byte, version int64) bool {
		if !bytes.HasPrefix(key, voteKey) {
			return false
		}

		voter := crypto.Address(key[len(prefixVote)+len(ConvIDFromUint(draftID)):])

		var vote types.Vote
		err := json.Unmarshal(value, &vote)
		if err != nil {
			return false
		}

		voteInfo = append(voteInfo, &types.VoteInfo{
			Voter: voter,
			Vote:  &vote,
		})

		return false
	})

	return voteInfo
}

func (s Store) DeleteVote(draftID uint32, voter crypto.Address) {
	s.remove(makeVoteKey(draftID, voter))
}

// Parcel store
func makeParcelKey(parcelID []byte) []byte {
	return append(prefixParcel, parcelID...)
}

func (s Store) SetParcel(parcelID []byte, value *types.Parcel) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.set(makeParcelKey(parcelID), b)
	return nil
}

func (s Store) GetParcel(parcelID []byte, committed bool) *types.Parcel {
	b := s.get(makeParcelKey(parcelID), committed)
	if len(b) == 0 {
		return nil
	}
	var parcel types.Parcel
	err := json.Unmarshal(b, &parcel)
	if err != nil {
		return nil
	}
	return &parcel
}

func (s Store) DeleteParcel(parcelID []byte) {
	s.remove(makeParcelKey(parcelID))
}

// Request store
func makeRequestKey(buyer crypto.Address, parcelID []byte) (buyerParcelKey, parcelBuyerKey []byte) {
	buyerParcelKey = append(prefixRequest, append(append(buyer, ':'), parcelID...)...)
	parcelBuyerKey = append(prefixRequest, append(append(parcelID, ':'), buyer...)...)
	return
}

func splitParcelBuyerKey(prefix, key []byte) (parcelID []byte, buyer crypto.Address) {
	// prefix + parcelID + buyer
	parcelID = key[len(prefix) : len(key)-crypto.AddressSize-1]
	buyer = key[len(key)-crypto.AddressSize:]
	return
}

func (s Store) SetRequest(buyer crypto.Address, parcelID []byte, value *types.Request) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	buyerParcelKey, parcelBuyerKey := makeRequestKey(buyer, parcelID)

	// parcelBuyerKey has only nil as value to use it as index
	s.set(buyerParcelKey, b)
	s.set(parcelBuyerKey, []byte{})

	return nil
}

func (s Store) GetRequest(buyer crypto.Address, parcelID []byte, committed bool) *types.Request {
	buyerParcelKey, _ := makeRequestKey(buyer, parcelID)

	b := s.get(buyerParcelKey, committed)
	if len(b) == 0 {
		return nil
	}
	var request types.Request
	err := json.Unmarshal(b, &request)
	if err != nil {
		return nil
	}
	return &request
}

func (s Store) GetRequests(parcelID []byte, committed bool) []*types.RequestEx {
	prefixRequestKey := append(prefixRequest, append(parcelID, ':')...)
	requests := []*types.RequestEx{}

	imt, err := s.getImmutableTree(committed)
	if err != nil {
		return nil
	}

	imt.IterateRangeInclusive(prefixRequestKey, nil, true,
		func(key []byte, value []byte, version int64) bool {
			if !bytes.HasPrefix(key, prefixRequestKey) {
				return false
			}

			// TODO: Is this really the best ?
			parcelID, buyer := splitParcelBuyerKey(prefixRequest, key)
			requestValue := s.GetRequest(buyer, parcelID, committed)
			if requestValue == nil {
				return false
			}
			request := types.RequestEx{
				Request: requestValue,
				Buyer:   buyer,
			}

			requests = append(requests, &request)

			return false
		},
	)

	return requests
}

func (s Store) DeleteRequest(buyer crypto.Address, parcelID []byte) {
	buyerParcelKey, parcelBuyerKey := makeRequestKey(buyer, parcelID)

	s.remove(buyerParcelKey)
	s.remove(parcelBuyerKey)
}

// Usage store
func makeUsageKey(buyer crypto.Address, parcelID []byte) (buyerParcelKey, parcelBuyerKey []byte) {
	buyerParcelKey = append(prefixUsage, append(append(buyer, ':'), parcelID...)...)
	parcelBuyerKey = append(prefixUsage, append(append(parcelID, ':'), buyer...)...)
	return
}

func (s Store) SetUsage(buyer crypto.Address, parcelID []byte, value *types.Usage) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	buyerParcelKey, parcelBuyerKey := makeUsageKey(buyer, parcelID)

	// parcelBuyerKey has only nil as value to use it as index
	s.set(buyerParcelKey, b)
	s.set(parcelBuyerKey, []byte{})

	return nil
}

func (s Store) GetUsage(buyer crypto.Address, parcelID []byte, committed bool) *types.Usage {
	buyerParcelKey, _ := makeUsageKey(buyer, parcelID)
	b := s.get(buyerParcelKey, committed)
	if len(b) == 0 {
		return nil
	}
	var usage types.Usage
	err := json.Unmarshal(b, &usage)
	if err != nil {
		return nil
	}
	return &usage
}

func (s Store) GetUsages(parcelID []byte, committed bool) []*types.UsageEx {
	prefixUsageKey := append(prefixUsage, append(parcelID, ':')...)
	usages := []*types.UsageEx{}

	imt, err := s.getImmutableTree(committed)
	if err != nil {
		return nil
	}

	imt.IterateRangeInclusive(prefixUsageKey, nil, true,
		func(key []byte, value []byte, version int64) bool {
			if !bytes.HasPrefix(key, prefixUsageKey) {
				return false
			}

			// TODO: Is this really the best ?
			parcelID, buyer := splitParcelBuyerKey(prefixUsage, key)
			usage := types.UsageEx{
				Usage: s.GetUsage(buyer, parcelID, committed),
				Buyer: buyer,
			}

			usages = append(usages, &usage)

			return false
		},
	)

	return usages
}

func (s Store) DeleteUsage(buyer crypto.Address, parcelID []byte) {
	buyerParcelKey, parcelBuyerKey := makeUsageKey(buyer, parcelID)

	s.remove(buyerParcelKey)
	s.remove(parcelBuyerKey)
}

func (s Store) GetValidators(max uint64, committed bool) abci.ValidatorUpdates {
	var vals abci.ValidatorUpdates
	stakes := s.GetTopStakes(max, nil, committed)
	adjFactor := calcAdjustFactor(stakes)
	for _, stake := range stakes {
		key := abci.PubKey{ // TODO
			Type: "ed25519",
			Data: stake.Validator[:],
		}
		var power big.Int
		power.Rsh(&stake.Amount.Int, adjFactor)
		val := abci.ValidatorUpdate{
			PubKey: key,
			Power:  power.Int64(),
		}
		if val.Power > 0 {
			vals = append(vals, val)
		}
	}
	return vals
}

func (s Store) Compact() {
	//fmt.Println("compacting")
	cleveldb, ok := s.merkleDB.(*tmdb.CLevelDB)
	if ok {
		//fmt.Println("cleveldb compacting")
		cleveldb.DB().CompactRange(levigo.Range{nil, nil})
	}
	cleveldb, ok = s.indexDB.(*tmdb.CLevelDB)
	if ok {
		//fmt.Println("cleveldb compacting")
		cleveldb.DB().CompactRange(levigo.Range{nil, nil})
	}
	cleveldb, ok = s.incentiveDB.(*tmdb.CLevelDB)
	if ok {
		//fmt.Println("cleveldb compacting")
		cleveldb.DB().CompactRange(levigo.Range{nil, nil})
	}
	cleveldb, ok = s.lazinessCounterDB.(*tmdb.CLevelDB)
	if ok {
		//fmt.Println("cleveldb compacting")
		cleveldb.DB().CompactRange(levigo.Range{nil, nil})
	}
}

func (s Store) Close() {
	s.merkleDB.Close()
	s.indexDB.Close()
	s.incentiveDB.Close()
	s.lazinessCounterDB.Close()
}

func calcAdjustFactor(stakes []*types.Stake) uint {
	var vp big.Int
	max := MaxTotalVotingPower
	var vps int64 = 0
	var shifts uint = 0
	for _, stake := range stakes {
		vp.Rsh(&stake.Amount.Int, shifts)
		for !vp.IsInt64() {
			vp.Rsh(&vp, 1)
			shifts++
			vps >>= 1
		}
		vpi := vp.Int64()
		tmp := vps + vpi
		for tmp < vps || tmp > max {
			vps >>= 1
			vpi >>= 1
			shifts++
			tmp = vps + vpi
		}
		vps = tmp
	}
	return shifts
}
