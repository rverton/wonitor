package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	badger "github.com/dgraph-io/badger"
	"github.com/stretchr/testify/assert"
)

func TempFileName(prefix, suffix string) string {
	randBytes := make([]byte, 16)
	rand.Read(randBytes)
	return filepath.Join(os.TempDir(), prefix+hex.EncodeToString(randBytes)+suffix)
}

func refreshDb() (string, *badger.DB, error) {
	fname := TempFileName("wonitor", "")
	db, err := initDb(fname)

	return fname, db, err
}

func bucketCount(db *badger.DB, name string) (int, error) {
	count := 0

	err := db.View(func(tx *badger.Txn) error {

		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			count++
		}

		return nil
	})

	return count, err
}

func TestAdd(t *testing.T) {
	fname, db, err := refreshDb()
	fmt.Println(fname)
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(fname)

	cnt, err := bucketCount(db, "urls")
	assert.Nil(t, err)
	assert.Equal(t, 0, cnt)

	addUrl(db, "https://robinverton.de", false)

	cnt, err = bucketCount(db, "urls")
	assert.Nil(t, err)
	assert.Equal(t, 1, cnt)
}

func TestRemove(t *testing.T) {
	fname, db, err := refreshDb()
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(fname)

	cnt, err := bucketCount(db, "urls")
	assert.Nil(t, err)
	assert.Equal(t, cnt, 0)

	addUrl(db, "https://robinverton.de", false)

	cnt, _ = bucketCount(db, "urls")
	assert.Equal(t, cnt, 1)

	removeUrl(db, "https://robinverton.de")

	cnt, _ = bucketCount(db, "urls")
	assert.Equal(t, cnt, 0)
}
