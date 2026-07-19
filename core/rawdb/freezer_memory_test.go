// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package rawdb

import (
	"testing"

	"github.com/ethereum/go-ethereum/core/rawdb/ancienttest"
	"github.com/ethereum/go-ethereum/ethdb"
)

func TestMemoryFreezer(t *testing.T) {
	ancienttest.TestAncientSuite(t, func(kinds []string) ethdb.AncientStore {
		tables := make(map[string]freezerTableConfig)
		for _, kind := range kinds {
			tables[kind] = freezerTableConfig{
				noSnappy:  true,
				tailGroup: ancienttest.TailGroup,
			}
		}
		return NewMemoryFreezer(false, tables)
	})
	ancienttest.TestResettableAncientSuite(t, func(kinds []string) ethdb.ResettableAncientStore {
		tables := make(map[string]freezerTableConfig)
		for _, kind := range kinds {
			tables[kind] = freezerTableConfig{
				noSnappy:  true,
				tailGroup: ancienttest.TailGroup,
			}
		}
		return NewMemoryFreezer(false, tables)
	})
}

func TestMemoryFreezerTruncateHeadBelowTail(t *testing.T) {
	const group = "test"
	f := NewMemoryFreezer(false, map[string]freezerTableConfig{
		"a": {noSnappy: true, tailGroup: group},
		"b": {noSnappy: true, tailGroup: group},
	})
	for i := uint64(0); i < 4; i++ {
		if _, err := f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
			if err := op.AppendRaw("a", i, []byte{byte(i)}); err != nil {
				return err
			}
			return op.AppendRaw("b", i, []byte{byte(i)})
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if _, err := f.TruncateTail(group, 2); err != nil {
		t.Fatalf("truncate tail: %v", err)
	}
	if _, err := f.TruncateHead(1); err != nil {
		t.Fatalf("truncate head below tail: %v", err)
	}
	if tail, err := f.Tail(group); err != nil || tail != 1 {
		t.Fatalf("tail after head truncation: got %d (err %v), want 1", tail, err)
	}
	if _, err := f.ModifyAncients(func(op ethdb.AncientWriteOp) error {
		if err := op.AppendRaw("a", 1, []byte("a")); err != nil {
			return err
		}
		return op.AppendRaw("b", 1, []byte("b"))
	}); err != nil {
		t.Fatalf("append after rewind: %v", err)
	}
	if got, err := f.Ancient("a", 1); err != nil || string(got) != "a" {
		t.Fatalf("read after rewind: got %q (err %v), want a", got, err)
	}
}

// TestMemoryFreezerBALStyleTruncateHeadBelowAlignedTail models the production
// path after BAL table introduction: an empty table is aligned so
// items==offset==oldHead, later freezes grow items while the tail stays, then
// TruncateHead rewinds below that tail. Both tables must agree and the group
// tail cache must refresh.
func TestMemoryFreezerBALStyleTruncateHeadBelowAlignedTail(t *testing.T) {
	f := NewMemoryFreezer(false, map[string]freezerTableConfig{
		"bodies": {noSnappy: true, tailGroup: "blockdata"},
		"bals":   {noSnappy: true, tailGroup: "bal"},
	})
	bodies := f.tables["bodies"]
	bals := f.tables["bals"]

	// bodies: items 0..99
	for i := 0; i < 100; i++ {
		bodies.data = append(bodies.data, []byte{byte(i)})
		bodies.size++
	}
	bodies.items = 100
	// bals aligned empty at head 100 (post-repair)
	bals.offset = 100
	bals.items = 100
	f.items = 100
	f.tails["bal"] = 100
	f.tails["blockdata"] = 0

	// later freezes: 20 more blocks
	for i := 0; i < 20; i++ {
		bodies.data = append(bodies.data, []byte{byte(i)})
		bodies.size++
		bals.data = append(bals.data, []byte{byte(i)})
		bals.size++
	}
	bodies.items = 120
	bals.items = 120
	f.items = 120

	if _, err := f.TruncateHead(51); err != nil {
		t.Fatalf("TruncateHead(51): %v", err)
	}
	if bodies.items != 51 || bals.items != 51 {
		t.Fatalf("inconsistent heads: bodies=%d bals=%d, want 51", bodies.items, bals.items)
	}
	if bals.offset != 51 {
		t.Fatalf("bals.offset=%d, want 51 after reset below old tail", bals.offset)
	}
	if tail := f.tails["bal"]; tail != 51 {
		t.Fatalf("bal group tail cache=%d, want 51", tail)
	}
}
