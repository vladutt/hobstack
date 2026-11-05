// winres scrie resursa Windows cu iconita aplicatiei (un obiect COFF .syso, pe care go build il ia singur
// cand construieste pentru windows/amd64). Ruleaza-l dupa ce schimbi assets/hobstack.ico:
//
//	go run ./scripts/winres assets/hobstack.ico cmd/hobstack/rsrc_windows_amd64.syso
//
// Doar biblioteca standard. Formatul: directorul de resurse (tip -> id -> limba -> date) din documentatia PE,
// cu iconitele ca RT_ICON si lista lor ca RT_GROUP_ICON; campurile cu adresa datelor (RVA) primesc relocari
// IMAGE_REL_AMD64_ADDR32NB fata de simbolul sectiunii .rsrc, iar linker-ul pune adresa reala.
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

const (
	rtIcon      = 3
	rtGroupIcon = 14
	langNeutral = 0x0409 // en-US, ca la majoritatea aplicatiilor
	relAddr32NB = 3      // IMAGE_REL_AMD64_ADDR32NB
	machineAMD  = 0x8664
)

type icoImage struct {
	width, height, colors byte
	planes, bitCount      uint16
	data                  []byte
}

// readICO citeste un fisier .ico (ICONDIR + intrari + imagini PNG sau BMP).
func readICO(b []byte) ([]icoImage, error) {
	if len(b) < 6 || binary.LittleEndian.Uint16(b[2:]) != 1 {
		return nil, errors.New("not an .ico file")
	}
	n := int(binary.LittleEndian.Uint16(b[4:]))
	var out []icoImage
	for i := range n {
		e := 6 + 16*i
		if e+16 > len(b) {
			return nil, errors.New("truncated .ico")
		}
		size := binary.LittleEndian.Uint32(b[e+8:])
		off := binary.LittleEndian.Uint32(b[e+12:])
		if uint64(off)+uint64(size) > uint64(len(b)) {
			return nil, errors.New("bad image offset in .ico")
		}
		out = append(out, icoImage{width: b[e], height: b[e+1], colors: b[e+2],
			planes: binary.LittleEndian.Uint16(b[e+4:]), bitCount: binary.LittleEndian.Uint16(b[e+6:]),
			data: b[off : off+size]})
	}
	return out, nil
}

// leaf: o resursa (tip, id) cu datele ei.
type leaf struct {
	typ, id uint32
	data    []byte
}

func groupIcon(imgs []icoImage) []byte {
	var g bytes.Buffer
	binary.Write(&g, binary.LittleEndian, [3]uint16{0, 1, uint16(len(imgs))})
	for i, im := range imgs {
		g.Write([]byte{im.width, im.height, im.colors, 0})
		binary.Write(&g, binary.LittleEndian, im.planes)
		binary.Write(&g, binary.LittleEndian, im.bitCount)
		binary.Write(&g, binary.LittleEndian, uint32(len(im.data)))
		binary.Write(&g, binary.LittleEndian, uint16(i+1)) // id-ul RT_ICON
	}
	return g.Bytes()
}

// rsrc construieste continutul sectiunii .rsrc si pozitiile campurilor care au nevoie de relocare.
func rsrc(leaves []leaf) ([]byte, []uint32) {
	// tipurile in ordine crescatoare (cerinta formatului), fiecare cu id-urile lui
	types := []uint32{}
	byType := map[uint32][]leaf{}
	for _, l := range leaves {
		if _, ok := byType[l.typ]; !ok {
			types = append(types, l.typ)
		}
		byType[l.typ] = append(byType[l.typ], l)
	}
	dirSize := func(entries int) int { return 16 + 8*entries }

	// asezarea: radacina, directoarele de tip, directoarele de id (cu cate o limba), intrarile de date, datele
	off := dirSize(len(types))
	typeOff := map[uint32]int{}
	for _, t := range types {
		typeOff[t] = off
		off += dirSize(len(byType[t]))
	}
	langOff := make([]int, len(leaves))
	for i := range leaves {
		langOff[i] = off
		off += dirSize(1)
	}
	entryOff := make([]int, len(leaves))
	for i := range leaves {
		entryOff[i] = off
		off += 16
	}
	dataOff := make([]int, len(leaves))
	for i, l := range leaves {
		off = (off + 7) &^ 7
		dataOff[i] = off
		off += len(l.data)
	}
	buf := make([]byte, off)
	le := binary.LittleEndian
	dir := func(at, entries int) {
		le.PutUint16(buf[at+14:], uint16(entries)) // NumberOfIdEntries; restul campurilor 0
	}
	entry := func(at int, id uint32, target int, sub bool) {
		le.PutUint32(buf[at:], id)
		v := uint32(target)
		if sub {
			v |= 0x80000000
		}
		le.PutUint32(buf[at+4:], v)
	}

	dir(0, len(types))
	k := 0
	for ti, t := range types {
		entry(16+8*ti, t, typeOff[t], true)
		dir(typeOff[t], len(byType[t]))
		for j := range byType[t] {
			// leaves sunt grupate pe tip in ordinea din byType; k numara in aceeasi ordine
			i := k
			k++
			entry(typeOff[t]+16+8*j, leaves[i].id, langOff[i], true)
			dir(langOff[i], 1)
			entry(langOff[i]+16, langNeutral, entryOff[i], false)
			le.PutUint32(buf[entryOff[i]:], uint32(dataOff[i])) // RVA: relocat
			le.PutUint32(buf[entryOff[i]+4:], uint32(len(leaves[i].data)))
			copy(buf[dataOff[i]:], leaves[i].data)
		}
	}
	relocs := make([]uint32, len(leaves))
	for i := range leaves {
		relocs[i] = uint32(entryOff[i])
	}
	return buf, relocs
}

// coff impacheteaza sectiunea .rsrc intr-un obiect COFF pentru amd64.
func coff(sec []byte, relocs []uint32) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	const fileHdr, secHdr = 20, 40
	rawAt := fileHdr + secHdr
	relAt := rawAt + len(sec)
	symAt := relAt + 10*len(relocs)
	// antetul fisierului
	binary.Write(&b, le, uint16(machineAMD))
	binary.Write(&b, le, uint16(1)) // o sectiune
	binary.Write(&b, le, uint32(0)) // TimeDateStamp: build reproductibil
	binary.Write(&b, le, uint32(symAt))
	binary.Write(&b, le, uint32(1)) // un simbol
	binary.Write(&b, le, uint16(0)) // SizeOfOptionalHeader
	binary.Write(&b, le, uint16(0)) // Characteristics
	// antetul sectiunii
	b.WriteString(".rsrc\x00\x00\x00")
	binary.Write(&b, le, uint32(0)) // VirtualSize
	binary.Write(&b, le, uint32(0)) // VirtualAddress
	binary.Write(&b, le, uint32(len(sec)))
	binary.Write(&b, le, uint32(rawAt))
	binary.Write(&b, le, uint32(relAt))
	binary.Write(&b, le, uint32(0)) // PointerToLinenumbers
	binary.Write(&b, le, uint16(len(relocs)))
	binary.Write(&b, le, uint16(0))
	binary.Write(&b, le, uint32(0x40000040)) // date initializate, citire
	b.Write(sec)
	for _, r := range relocs {
		binary.Write(&b, le, r)
		binary.Write(&b, le, uint32(0)) // simbolul 0 = .rsrc
		binary.Write(&b, le, uint16(relAddr32NB))
	}
	// simbolul sectiunii
	b.WriteString(".rsrc\x00\x00\x00")
	binary.Write(&b, le, uint32(0)) // Value
	binary.Write(&b, le, uint16(1)) // SectionNumber
	binary.Write(&b, le, uint16(0)) // Type
	b.WriteByte(3)                  // IMAGE_SYM_CLASS_STATIC
	b.WriteByte(0)                  // fara simboluri auxiliare
	binary.Write(&b, le, uint32(4)) // tabela de siruri goala
	return b.Bytes()
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/winres <icon.ico> <out.syso>")
		os.Exit(2)
	}
	ico, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	imgs, err := readICO(ico)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var leaves []leaf
	for i, im := range imgs {
		leaves = append(leaves, leaf{typ: rtIcon, id: uint32(i + 1), data: im.data})
	}
	leaves = append(leaves, leaf{typ: rtGroupIcon, id: 1, data: groupIcon(imgs)})
	sec, relocs := rsrc(leaves)
	if err := os.WriteFile(os.Args[2], coff(sec, relocs), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s: %d icons, %d bytes of resources\n", os.Args[2], len(imgs), len(sec))
}
