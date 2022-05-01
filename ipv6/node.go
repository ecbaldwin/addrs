package ipv6

import (
	"fmt"
)

type trieNode struct {
	Prefix   Prefix
	Data     interface{}
	size     uint32
	h        uint16
	isActive bool
	children [2]*trieNode
}

func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func intMax(a, b int) int {
	if a < b {
		return b
	}
	return a
}

// contains is a helper which compares to see if the shorter prefix contains the
// longer.
//
// This function is not generally safe. It assumes non-nil pointers and that
// smaller.length < larger.length.
//
// `matches`: is true if the shorter key is a prefix of the longer key.
// `exact`: is true if the two keys are exactly the same (implies `matches`)
// `common`: is always the number of bits that the two keys have in common
// `child`: tells whether the first non-common bit in `longer` is a 0 or 1. It
//          is only valid if either `matches` or `exact` is false. The
//          following table describes how to interpret results.

// | matches | exact | child | note
// |---------|-------|-------|-------
// | false   | NA    | 0     | the two are disjoint and `longer` compares less than `shorter`
// | false   | NA    | 1     | the two are disjoint and `longer` compares greater than `shorter`
// | true    | false | 0     | `longer` belongs in `shorter`'s `children[0]`
// | true    | false | 1     | `longer` belongs in `shorter`'s `children[1]`
// | true    | true  | NA    | `shorter` and `longer` are the same key
func contains(shorter, longer Prefix) (matches, exact bool, common uint32, child int) {
	mask := uint128{0xffffffffffffffff, 0xffffffffffffffff}.leftShift(int(128 - shorter.length))

	matches = shorter.addr.ui.and(mask) == longer.addr.ui.and(mask)
	if matches {
		exact = shorter.length == longer.length
		common = shorter.length
	} else {
		common = uint32(shorter.addr.ui.xor(longer.addr.ui).leadingZeros())
	}
	if !exact {
		// Whether `longer` goes on the left (0) or right (1)
		pivotMask := uint128{0x8000000000000000, 0}.rightShift(int(common))
		if (longer.addr.ui.and(pivotMask) != uint128{}) {
			child = 1
		}
	}
	return
}

const (
	compareSame        int = iota
	compareContains        // Second key is a subset of the first
	compareIsContained     // Second key is a superset of the first
	compareDisjoint
)

// compare is a helper which compares two keys to find their relationship
func compare(a, b Prefix) (result int, reversed bool, common uint32, child int) {
	var aMatch, bMatch bool
	// Figure out which is the longer prefix and reverse them if b is shorter
	reversed = b.length < a.length
	if reversed {
		bMatch, aMatch, common, child = contains(b, a)
	} else {
		aMatch, bMatch, common, child = contains(a, b)
	}
	switch {
	case aMatch && bMatch:
		result = compareSame
	case aMatch && !bMatch:
		result = compareContains
	case !aMatch && bMatch:
		result = compareIsContained
	case !aMatch && !bMatch:
		result = compareDisjoint
	}
	return
}

func (me *trieNode) mutate(mutator func(*trieNode)) *trieNode {
	if me == nil {
		return nil
	}

	mutator(me)

	numNodes := me.children[0].NumNodes() + me.children[1].NumNodes()
	height := 1 + intMax(me.children[0].height(), me.children[1].height())

	me.size = uint32(numNodes)
	me.h = uint16(height)
	if me.isActive {
		me.size++
	}
	return me
}

func (me *trieNode) copyMutate(mutator func(*trieNode)) *trieNode {
	if me == nil {
		return nil
	}
	doppelganger := &trieNode{}
	*doppelganger = *me
	mutated := doppelganger.mutate(mutator)
	if *mutated == *me {
		return me
	}
	return mutated
}

type comparator func(a, b interface{}) bool

// Equal returns true if all of the entries are the same in the two data structures
func (me *trieNode) Equal(other *trieNode, eq comparator) bool {
	switch {
	case me == other:
		return true

	case me == nil:
		return false
	case other == nil:
		return false
	case me.isActive != other.isActive:
		return false
	case me.Prefix != other.Prefix:
		return false
	case me.isActive && !eq(me.Data, other.Data):
		return false
	case !me.children[0].Equal(other.children[0], eq):
		return false
	case !me.children[1].Equal(other.children[1], eq):
		return false

	default:
		return true
	}
}

// GetOrInsert returns the existing value if an exact match is found, otherwise, inserts the given default
func (me *trieNode) GetOrInsert(searchKey Prefix, data interface{}) (head, result *trieNode) {
	defer func() {
		if result == nil {
			result = &trieNode{Prefix: searchKey, Data: data}

			var err error
			head, err = me.insert(result, insertOpts{insert: true})
			if err != nil {
				// when getting *or* inserting, we design around the errors that could come from insert
				panic(fmt.Errorf("this error shouldn't happen: %w", err))
			}
		}
	}()

	if me == nil || searchKey.length < me.Prefix.length {
		return
	}

	matches, exact, _, child := contains(me.Prefix, searchKey)
	if !matches {
		return
	}

	if !exact {
		var newChild *trieNode
		newChild, result = me.children[child].GetOrInsert(searchKey, data)

		head = me.copyMutate(func(n *trieNode) {
			n.children[child] = newChild
		})
		return
	}

	if !me.isActive {
		return
	}

	return me, me
}

// Match returns the existing entry with the longest prefix that fully contains
// the prefix given by the key argument or nil if none match.
//
// "contains" means that the first "length" bits in the entry's key are exactly
// the same as the same number of first bits in the given search key. This
// implies the search key is at least as long as any matching node's prefix.
//
// Some examples include the following ipv4 and ipv6 matches:
//     10.0.0.0/24 contains 10.0.0.0/24, 10.0.0.0/25, and 10.0.0.0/32
//     2001:cafe:beef::/64 contains 2001:cafe:beef::a/124
//
// "longest" means that if multiple existing entries in the trie match the one
// with the longest length will be returned. It is the most specific match.
func (me *trieNode) Match(searchKey Prefix) *trieNode {
	if me == nil {
		return nil
	}

	nodeKey := me.Prefix
	if searchKey.length < nodeKey.length {
		return nil
	}

	matches, exact, _, child := contains(nodeKey, searchKey)
	if !matches {
		return nil
	}

	if !exact {
		if better := me.children[child].Match(searchKey); better != nil {
			return better
		}
	}

	if !me.isActive {
		return nil
	}

	return me
}

// IsEmpty returns whether the number of IP addresses is equal to zero
func (me *trieNode) IsEmpty() bool {
	if me == nil {
		return true
	}
	if me.isActive {
		return false
	}
	return (me.children[0].IsEmpty() && me.children[1].IsEmpty())
}

// NumNodes returns the number of entries in the trie
func (me *trieNode) NumNodes() int64 {
	if me == nil {
		return 0
	}
	return int64(me.size)
}

// height returns the maximum height of the trie.
func (me *trieNode) height() int {
	if me == nil {
		return 0
	}
	return int(me.h)
}

// isValid returns true if the tree is valid
// this method is only for unit tests to check the integrity of the structure
func (me *trieNode) isValid() bool {
	return me.isValidLen(0)
}

func (me *trieNode) isValidLen(minLen uint32) bool {
	if me == nil {
		return true
	}
	left, right := me.children[0], me.children[1]
	size := me.size
	if me.isActive {
		size--
	} else {
		if left == nil || right == nil {
			// Any child node should have been pulled up since this node isn't active
			return false
		}
	}
	if size != uint32(left.NumNodes()+right.NumNodes()) {
		return false
	}
	if me.h != 1+uint16(uint16(intMax(left.height(), right.height()))) {
		return false
	}
	if me.Prefix.length < minLen {
		return false
	}
	return left.isValidLen(me.Prefix.length+1) && right.isValidLen(me.Prefix.length+1)
}

// Update updates the key / value only if the key already exists
func (me *trieNode) Update(key Prefix, data interface{}, eq comparator) (newHead *trieNode, err error) {
	return me.insert(&trieNode{Prefix: key, Data: data}, insertOpts{update: true, eq: eq})
}

// InsertOrUpdate inserts the key / value if the key didn't previously exist.
// Otherwise, it updates the data.
func (me *trieNode) InsertOrUpdate(key Prefix, data interface{}, eq comparator) (newHead *trieNode) {
	var err error
	newHead, err = me.insert(&trieNode{Prefix: key, Data: data}, insertOpts{insert: true, update: true, eq: eq})
	if err != nil {
		// when inserting *or* updating, we design around the errors that could come from insert
		panic(fmt.Errorf("this error shouldn't happen: %w", err))
	}
	return newHead
}

// Insert is the public form of insert(...)
func (me *trieNode) Insert(key Prefix, data interface{}) (newHead *trieNode, err error) {
	return me.insert(&trieNode{Prefix: key, Data: data}, insertOpts{insert: true})
}

type insertOpts struct {
	insert, update bool
	eq             comparator
}

// insert adds a node into the trie and return the new root of the trie. It is
// important to note that the root of the trie can change. If the new node
// cannot be inserted, nil is returned.
func (me *trieNode) insert(node *trieNode, opts insertOpts) (newHead *trieNode, err error) {
	if me == nil {
		if !opts.insert {
			return me, fmt.Errorf("the key doesn't exist to update")
		}
		node = node.mutate(func(n *trieNode) {
			n.isActive = true
		})
		return node, nil
	}

	// Test containership both ways
	result, reversed, common, child := compare(me.Prefix, node.Prefix)
	switch result {
	case compareSame:
		// They have the same key
		if me.isActive && !opts.update {
			return me, fmt.Errorf("a node with that key already exists")
		}
		if !me.isActive && !opts.insert {
			return me, fmt.Errorf("the key doesn't exist to update")
		}
		return node.mutate(func(n *trieNode) {
			if me.isActive && opts.eq(me.Data, node.Data) {
				node.Data = me.Data
			}
			n.children = me.children
			n.isActive = true
		}), nil

	case compareContains:
		// Trie node's key contains the new node's key. Insert it recursively.
		newChild, err := me.children[child].insert(node, opts)
		if err != nil {
			return me, err
		}
		newNode := me.copyMutate(func(n *trieNode) {
			n.children[child] = newChild
		})
		return newNode, nil

	case compareIsContained:
		// New node's key contains the trie node's key. Insert new node as the parent of the trie.
		if !opts.insert {
			return me, fmt.Errorf("the key doesn't exist to update")
		}
		node = node.mutate(func(n *trieNode) {
			n.children[child] = me
			n.isActive = true
		})
		return node, nil

	case compareDisjoint:
		// Keys are disjoint. Create a new (inactive) parent node to join them side-by-side.
		var newChild *trieNode
		newChild, err := newChild.insert(node, opts)
		if err != nil {
			return me, err
		}

		var children [2]*trieNode

		if (child == 1) != reversed { // (child == 1) XOR reversed
			children[0], children[1] = me, newChild
		} else {
			children[0], children[1] = newChild, me
		}

		newNode := &trieNode{
			Prefix: Prefix{
				addr: Address{
					ui: me.Prefix.addr.ui.and(uint128{0xffffffffffffffff, 0xffffffffffffffff}.rightShift(int(common)).complement()), // zero out bits not in common
				},
				length: common,
			},
			children: children,
		}
		newNode.mutate(func(n *trieNode) {})
		return newNode, nil
	}
	panic("unreachable code")
}

type deleteOpts struct {
	flatten bool
}

// Delete removes a node from the trie given a key and returns the new root of
// the trie. It is important to note that the root of the trie can change.
func (me *trieNode) Delete(key Prefix) (newHead *trieNode, err error) {
	return me.del(key, deleteOpts{})
}

func reverseChild(child int) int {
	return (child + 1) % 2
}

func (me *trieNode) del(key Prefix, opts deleteOpts) (newHead *trieNode, err error) {
	if me == nil {
		if opts.flatten {
			return nil, nil
		}
		return me, fmt.Errorf("cannot delete from a nil")
	}

	result, _, _, child := compare(me.Prefix, key)
	switch result {
	case compareSame:
		if opts.flatten {
			return nil, nil
		}
		// Delete this node
		if me.children[0] == nil {
			// At this point, it doesn't matter if it is nil or not
			return me.children[1], nil
		}
		if me.children[1] == nil {
			return me.children[0], nil
		}

		// The two children are disjoint so keep this inactive node.
		newNode := me.copyMutate(func(n *trieNode) {
			n.isActive = false
			n.Data = nil
		})
		return newNode, nil

	case compareContains:
		// Delete recursively.
		newChild, err := me.children[child].del(key, opts)
		if err != nil {
			return me, err
		}

		if newChild == nil && !me.isActive {
			// Promote the other child up
			return me.children[reverseChild(child)], nil
		}
		newNode := me.copyMutate(func(n *trieNode) {
			n.children[child] = newChild
		})
		return newNode, nil

	case compareIsContained:
		if opts.flatten {
			return nil, nil
		}
		return me, fmt.Errorf("key not found")

	case compareDisjoint:
		return me, fmt.Errorf("key not found")
	}
	panic("unreachable code")
}

// active returns whether a node represents an active prefix in the tree (true)
// or an intermediate node (false). It is safe to call on a nil pointer.
func (me *trieNode) active() bool {
	if me == nil {
		return false
	}
	return me.isActive
}
