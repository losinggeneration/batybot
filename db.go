package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	bolt "go.etcd.io/bbolt"
)

type Timestamp time.Time

func (t Timestamp) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", t.String())), nil
}

func (t *Timestamp) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	p, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return err
	}

	*t = Timestamp(p)

	return nil
}

func (t Timestamp) String() string {
	return time.Time(t).Format(time.RFC3339Nano)
}

type UserTotals struct {
	Tier1 int64
	Tier2 int64
	Tier3 int64
	Bits  int64
	Tips  decimal.Decimal
}

type Follows struct {
	Username  string
	Timestamp time.Time
}

type Raids struct {
	Username  string
	Timestamp time.Time
}

func ptr[T any](v T) *T {
	return &v
}

type DB struct {
	*bolt.DB
}

func (d *DB) put(bucket string, key string, value any) error {
	return d.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))

		v, err := json.Marshal(value)
		if err != nil {
			return err
		}

		return b.Put([]byte(key), v)
	})
}

func (d *DB) setupBuckets() error {
	return d.Update(func(tx *bolt.Tx) error {
		var errs error
		for _, bucket := range []string{"follows", "raids", "users"} {
			_, err := tx.CreateBucketIfNotExists([]byte(bucket))
			if err != nil {
				errs = fmt.Errorf("%w: Unable to create %q bucket: %w", errs, bucket, err)
			}
		}

		return errs
	})
}

func (d *DB) getUsers() (map[string]UserTotals, error) {
	users := make(map[string]UserTotals)
	err := d.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("users"))
		return b.ForEach(func(k []byte, v []byte) error {
			var user UserTotals
			if err := json.Unmarshal(v, &user); err != nil {
				return err
			}

			users[string(k)] = user

			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	return users, err
}

func (d *DB) getTotals() (*UserTotals, error) {
	var totals UserTotals
	err := d.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("users"))
		return b.ForEach(func(k []byte, v []byte) error {
			var user UserTotals
			if err := json.Unmarshal(v, &user); err != nil {
				return err
			}

			totals.Tier1 += user.Tier1
			totals.Tier2 += user.Tier2
			totals.Tier3 += user.Tier3
			totals.Bits += user.Bits
			totals.Tips = totals.Tips.Add(user.Tips)

			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	return &totals, nil
}

func (d *DB) addUser(username string, totals UserTotals) error {
	username = strings.ToLower(username)
	return d.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("users"))
		var current UserTotals
		if data := b.Get([]byte(username)); data != nil {
			if err := json.Unmarshal(data, &current); err != nil {
				return err
			}
		}

		current.Tier1 += totals.Tier1
		current.Tier2 += totals.Tier2
		current.Tier3 += totals.Tier3
		current.Bits += totals.Bits
		current.Tips = current.Tips.Add(totals.Tips)

		value, err := json.Marshal(current)
		if err != nil {
			return err
		}

		return b.Put([]byte(username), value)
	})
}

func (d *DB) addRaid(username string, t *time.Time) error {
	username = strings.ToLower(username)
	if t == nil {
		t = ptr(time.Now())
	}

	value := Raids{
		Username:  username,
		Timestamp: *t,
	}

	key := Timestamp(*t).String()
	return d.put("raids", key, value)
}

func (d *DB) addFollow(username string, t *time.Time) error {
	username = strings.ToLower(username)
	if t == nil {
		t = ptr(time.Now())
	}

	value := Follows{
		Username:  username,
		Timestamp: *t,
	}

	key := Timestamp(*t).String()
	return d.put("follows", key, value)
}
