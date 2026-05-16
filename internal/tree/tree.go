package tree

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"unsafe"

	"github.com/vinnedev/rinha-2026/internal/domain"
)

const (
	Magic     = uint32(0x54524545)
	VersionDT = uint32(1)
	VersionRF = uint32(2)
	HeaderDT  = 12
	HeaderRF  = 16
	NodeSz    = 20
)

type node struct {
	feature   int16
	_pad      int16
	threshold float32
	left      int32
	right     int32
	proba     float32
}

type Tree struct {
	roots    []int32
	allNodes []node
	raw      []byte
	invN     float32
}

func Load(path string) (*Tree, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("tree read: %w", err)
	}
	if len(raw) < HeaderDT {
		return nil, errors.New("tree: file too small")
	}
	if binary.LittleEndian.Uint32(raw[0:4]) != Magic {
		return nil, errors.New("tree: bad magic")
	}
	version := binary.LittleEndian.Uint32(raw[4:8])
	switch version {
	case VersionDT:
		return loadDT(raw)
	case VersionRF:
		return loadRF(raw)
	default:
		return nil, fmt.Errorf("tree: unknown version %d", version)
	}
}

func loadDT(raw []byte) (*Tree, error) {
	n := int(binary.LittleEndian.Uint32(raw[8:12]))
	want := HeaderDT + n*NodeSz
	if len(raw) < want {
		return nil, fmt.Errorf("tree: truncated %d < %d", len(raw), want)
	}
	ptr := (*node)(unsafe.Pointer(&raw[HeaderDT]))
	nodes := unsafe.Slice(ptr, n)
	return &Tree{
		roots:    []int32{0},
		allNodes: nodes,
		raw:      raw,
		invN:     1.0,
	}, nil
}

func loadRF(raw []byte) (*Tree, error) {
	if len(raw) < HeaderRF {
		return nil, errors.New("tree: rf header truncated")
	}
	nTrees := int(binary.LittleEndian.Uint32(raw[8:12]))
	totalNodes := int(binary.LittleEndian.Uint32(raw[12:16]))

	tableOff := HeaderRF
	tableEnd := tableOff + nTrees*4
	if len(raw) < tableEnd {
		return nil, errors.New("tree: rf tree-table truncated")
	}
	roots := make([]int32, nTrees)
	cursor := int32(0)
	sumNodes := 0
	for i := 0; i < nTrees; i++ {
		nNodes := int(binary.LittleEndian.Uint32(raw[tableOff+i*4:]))
		roots[i] = cursor
		cursor += int32(nNodes)
		sumNodes += nNodes
	}
	if sumNodes != totalNodes {
		return nil, fmt.Errorf("tree: rf total_nodes mismatch %d vs %d", sumNodes, totalNodes)
	}
	nodesOff := tableEnd
	want := nodesOff + totalNodes*NodeSz
	if len(raw) < want {
		return nil, fmt.Errorf("tree: rf truncated %d < %d", len(raw), want)
	}
	ptr := (*node)(unsafe.Pointer(&raw[nodesOff]))
	nodes := unsafe.Slice(ptr, totalNodes)
	return &Tree{
		roots:    roots,
		allNodes: nodes,
		raw:      raw,
		invN:     1.0 / float32(nTrees),
	}, nil
}

func (t *Tree) Close() error {
	if t == nil {
		return nil
	}
	t.allNodes = nil
	t.roots = nil
	t.raw = nil
	return nil
}

func (t *Tree) NodeCount() int {
	if t == nil {
		return 0
	}
	return len(t.allNodes)
}

func (t *Tree) TreeCount() int {
	if t == nil {
		return 0
	}
	return len(t.roots)
}

func (t *Tree) Predict(features [domain.Dim]float32) float32 {
	if len(t.roots) == 1 {
		return walk(t.allNodes, t.roots[0], features)
	}
	var sum float32
	for _, r := range t.roots {
		sum += walk(t.allNodes, r, features)
	}
	return sum * t.invN
}

func walk(nodes []node, root int32, features [domain.Dim]float32) float32 {
	i := root
	for {
		n := &nodes[i]
		if n.feature < 0 {
			return n.proba
		}
		if features[n.feature] <= n.threshold {
			i = root + n.left
		} else {
			i = root + n.right
		}
	}
}
