package store

import "strings"

// Markers: liniile (numerotate de la 1) din versiunea de acum a fisierului, schimbate de un task.
type Markers struct {
	Added    []int `json:"added"`
	Modified []int `json:"modified"`
	Deleted  []int `json:"deleted"` // linia inaintea careia s-a sters ceva (len+1 = la sfarsit)
}

// bitii unei linii in calcul
const (
	markAdded    = 1
	markModified = 2
	markDeleted  = 4 // s-a sters ceva chiar inaintea liniei
)

// FileMarkers aplica in ordine fragmentele de diff ale unui fisier (modificarile unui task, deja
// filtrate pe cale) si intoarce liniile marcate in versiunea finala. exact=false cand un fragment
// nu are numere de linie, e trunchiat sau un Write a rescris fisierul fara fragmente: atunci nu
// se poate spune unde sunt liniile, deci nu intoarcem marcaje.
func FileMarkers(changes []TaskChange) (Markers, bool) {
	var marks []uint8 // marks[i] = linia i+1 din versiunea curenta; lipsa = nemarcata
	for _, c := range changes {
		if c.Truncated || (c.Kind != "create" && len(c.Patch) == 0) {
			return Markers{Added: []int{}, Modified: []int{}, Deleted: []int{}}, false
		}
		// fragmentele unei modificari sunt in ordine; aplicate de jos in sus, pozitiile vechi raman valabile
		for i := len(c.Patch) - 1; i >= 0; i-- {
			h := c.Patch[i]
			if h.OldStart == 0 && h.NewStart == 0 {
				return Markers{Added: []int{}, Modified: []int{}, Deleted: []int{}}, false
			}
			marks = applyHunk(marks, h)
		}
	}
	out := Markers{Added: []int{}, Modified: []int{}, Deleted: []int{}}
	for i, m := range marks {
		if m&markAdded != 0 {
			out.Added = append(out.Added, i+1)
		} else if m&markModified != 0 {
			out.Modified = append(out.Modified, i+1)
		}
		if m&markDeleted != 0 {
			out.Deleted = append(out.Deleted, i+1)
		}
	}
	return out, true
}

// applyHunk inlocuieste in marks zona veche a fragmentului cu liniile noi, marcate.
func applyHunk(marks []uint8, h Hunk) []uint8 {
	// indexul primei linii vechi din fragment. In structuredPatch (jsdiff, ce intoarce Claude) si un fragment
	// doar cu linii noi (OldLines == 0) are OldStart = linia inaintea careia intra; {0,0,1,N} (createHunk) da 0.
	pos := h.OldStart - 1
	if pos < 0 {
		pos = 0
	}
	for len(marks) < pos+h.OldLines+1 {
		marks = append(marks, 0) // fisierul e mai lung decat ce stim: liniile nemarcate
	}
	old := marks[pos : pos+h.OldLines]
	next := []uint8{}
	oi := 0      // linia veche curenta din fragment
	pending := 0 // linii "-" inca neinlocuite de un "+"
	carry := uint8(0)
	for _, l := range h.Lines {
		if l == "" || strings.HasPrefix(l, "\\") {
			continue
		}
		switch l[0] {
		case '-':
			if oi < len(old) {
				carry |= old[oi] & markDeleted // un marcaj de stergere de pe linia scoasa trece mai jos
			}
			oi++
			pending++
		case '+':
			m := uint8(markAdded)
			if pending > 0 {
				pending--
				m = markModified
			}
			// o linie adaugata de task si inlocuita tot de el ramane "added"
			if m == markModified && oi > 0 && oi-1 < len(old) && old[oi-1]&markAdded != 0 {
				m = markAdded
			}
			next = append(next, m|carry)
			carry = 0
		default: // context
			m := uint8(0)
			if oi < len(old) {
				m = old[oi]
			}
			oi++
			if pending > 0 {
				m |= markDeleted // s-au sters linii fara inlocuire, chiar inaintea acestei linii
				pending = 0
			}
			next = append(next, m|carry)
			carry = 0
		}
	}
	rest := marks[pos+h.OldLines:]
	if pending > 0 || carry != 0 {
		// stergere la capatul fragmentului: marcajul trece pe prima linie de dupa el
		if len(rest) == 0 {
			rest = []uint8{0}
		}
		rest = append([]uint8{rest[0] | markDeleted | carry}, rest[1:]...)
	}
	out := make([]uint8, 0, len(marks)-h.OldLines+len(next))
	out = append(out, marks[:pos]...)
	out = append(out, next...)
	return append(out, rest...)
}
