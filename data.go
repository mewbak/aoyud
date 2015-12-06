// Program data storage.
//
// Since we don't do actual assembling of instruction mnemonics into opcodes,
// we can't completely rebuild a program's entire data.
// However, we only actually need the correct memory representation for a
// single reason - the correct identification of array boundaries, which are
// usually only implied in assembly syntax (by giving a name to the first
// element, then emitting N more unnamed elements following it). Therefore,
// it's enough to merely store all successive data initializations into a
// single chunk of bytes, and start a new one on every non-data instruction.

package main

import (
	"fmt"
	"strings"
)

// EmissionTarget represents a container that can hold data declarations, i.e.
// a segment or structure.
type EmissionTarget interface {
	Name() string
	// Offset returns the chunk and offset at the end of the emission target's
	// data block.
	Offset() (chunk uint, off uint64)
	// AddPointer adds the given pointer to the global symbol table (if the
	// symbol is supposed to be public) or the type's own one (if it has one).
	AddPointer(p *parser, sym string, ptr asmDataPtr) (err ErrorList)
	// AddData appends the given blob to the emission target's data block.
	AddData(blob Emittable) (err ErrorList)
	// WordSize returns the maximum number of bytes allowed for addresses.
	WordSize() uint8
}

// BlobList lists all blobs of a single data chunk by storing the same blob
// pointer for every byte it occupies. This allows easy random access of each
// byte within a chunk while also simplifying access to neighboring blobs.
type BlobList []*Emittable

func (l BlobList) Append(blob Emittable) BlobList {
	for i := uint(0); i < blob.Len(); i++ {
		l = append(l, &blob)
	}
	return l
}

func (l BlobList) Emit() (ret []byte) {
	var last *Emittable = nil
	for _, cur := range l {
		if cur != last {
			ret = append(ret, (*cur).Emit()...)
			last = cur
		}
	}
	return ret
}

// asmDataPtr represents a pointer to data in a specific segment or structure.
type asmDataPtr struct {
	et    EmissionTarget
	chunk uint
	off   *uint64 // nil = unknown position (used during pass 1)
	w     uint
}

func (p asmDataPtr) Thing() string {
	return "data pointer"
}

func (p asmDataPtr) String() string {
	var offChars int = int(p.et.WordSize() * 2)
	var offStr string
	if p.off == nil {
		offStr = strings.Repeat("?", offChars)
	} else {
		offStr = fmt.Sprintf("%0*xh", offChars, *p.off)
	}
	return fmt.Sprintf("(%d*) %s:%d:", p.w, p.et.Name(), p.chunk) + offStr
}

func (p asmDataPtr) width() uint {
	return p.w
}

type asmSegment struct {
	name       string
	chunks     []BlobList  // List of all contiguous data blocks
	prev       *asmSegment // in order to easily handle nested segments
	overflowed bool
	wordsize   uint8
}

func (s asmSegment) Thing() string      { return "segment name" }
func (s asmSegment) OpenThing() string  { return "open segment" }
func (s asmSegment) OpenThings() string { return "open segments" }
func (s asmSegment) Name() string       { return s.name }
func (s asmSegment) WordSize() uint8    { return s.wordsize }

func (s asmSegment) Prev() Nestable {
	if s.prev != nil {
		return s.prev
	}
	return nil
}

func (s asmSegment) String() string {
	return fmt.Sprintf(
		"SEGMENT (%d-bit, %d bytes of data in %d chunks)",
		s.wordsize*8, s.width(), len(s.chunks),
	)
}

func (s asmSegment) width() uint {
	ret := 0
	for _, c := range s.chunks {
		ret += len(c)
	}
	return uint(ret)
}

func (s *asmSegment) AddData(blob Emittable) (err ErrorList) {
	maxSize := uint64((1 << (s.wordsize * 8)) - 1)
	if uint64(blob.Len()+s.width()) > maxSize && !s.overflowed {
		s.overflowed = true
		err = err.AddF(ESError,
			"declaration overflows %d-bit segment: %s", s.wordsize*8, s.Name(),
		)
	}
	if len(s.chunks) == 0 {
		s.chunks = make([]BlobList, 1)
	}
	chunk := len(s.chunks) - 1
	s.chunks[chunk] = s.chunks[chunk].Append(blob)
	return err
}

func (s *asmSegment) Offset() (chunk uint, off uint64) {
	if len(s.chunks) != 0 {
		chunk = uint(len(s.chunks) - 1)
		off = uint64(len(s.chunks[chunk]))
	}
	return chunk, off
}

func (s *asmSegment) AddPointer(p *parser, sym string, ptr asmDataPtr) (err ErrorList) {
	return p.syms.Set(sym, ptr, true)
}

func (p *parser) CurrentEmissionTarget() EmissionTarget {
	// It is possible to open structures inside segments, but not vice versa.
	if p.struc != nil {
		return p.struc
	}
	return p.seg
}

func (p *parser) EmitPointer(sym string, width uint) (err ErrorList) {
	if sym == "" {
		return err
	}
	et := p.CurrentEmissionTarget()
	chunk, off := et.Offset()
	ptr := asmDataPtr{et: et, chunk: chunk, w: width}
	if p.pass2 {
		ptr.off = &off
	}
	return et.AddPointer(p, sym, ptr)
}
