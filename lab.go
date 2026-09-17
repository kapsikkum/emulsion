package main

// Lab scanner exports: recognise order and roll numbers in file names, so a lab download splits into
// its rolls and gets names worth browsing.

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Frontier exports name frames ORDER-R<roll>-<scan sequence>-<film edge frame>, e.g. B001738-R1-00-36A.JPG.
var frontierName = regexp.MustCompile(`(?i)^([a-z]*\d+)-r(\d+)-(\d+)-(\d+[a-z]?|[a-z]{1,2})$`)

type labFrame struct {
	Order string
	Roll  int
	Seq   int
	Edge  string // frame number printed on the film edge, e.g. "36A"
}

func parseLabName(name string) (labFrame, bool) {
	m := frontierName.FindStringSubmatch(strings.TrimSuffix(name, filepath.Ext(name)))
	if m == nil {
		return labFrame{}, false
	}
	roll, _ := strconv.Atoi(m[2])
	seq, _ := strconv.Atoi(m[3])
	return labFrame{Order: strings.ToUpper(m[1]), Roll: roll, Seq: seq, Edge: strings.ToUpper(m[4])}, true
}

func (f labFrame) key() string { return fmt.Sprintf("%s-R%d", f.Order, f.Roll) }

// rollKeys gives each file name a roll label, or nil when the names describe a single roll.
// Frontier-style names group by order and roll. Other scanners' names group by their prefix when
// prefixes differ only in their digits (R1-00123-0001 / R2-00123-0001, 000012340001 / 000012350001).
func rollKeys(names []string) []string {
	keys := make([]string, len(names))
	for i, n := range names {
		f, ok := parseLabName(n)
		if !ok {
			return prefixSplit(names)
		}
		keys[i] = f.key()
	}
	if distinct(keys) < 2 {
		return nil
	}
	return keys
}

var trailingNumber = regexp.MustCompile(`^(.*?)(\d+)([a-zA-Z]{0,2})$`)

func prefixSplit(names []string) []string {
	// Frame numbers are the trailing digits; long digit-only names hide the roll number in front of them.
	for _, width := range []int{0, 4, 3, 2} {
		keys := make([]string, len(names))
		frames := map[string][]int{}
		ok := true
		for i, name := range names {
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			m := trailingNumber.FindStringSubmatch(stem)
			if m == nil {
				ok = false
				break
			}
			prefix, digits := m[1], m[2]
			if width > 0 {
				if len(digits) < width+2 {
					ok = false
					break
				}
				prefix, digits = prefix+digits[:len(digits)-width], digits[len(digits)-width:]
			}
			n, _ := strconv.Atoi(digits)
			keys[i] = strings.TrimRight(prefix, "-_. ")
			frames[keys[i]] = append(frames[keys[i]], n)
		}
		if !ok || len(frames) < 2 {
			continue
		}
		if looksLikeRolls(frames) {
			return keys
		}
	}
	return nil
}

// looksLikeRolls accepts groups whose labels differ only in digits and that each hold 3+ mostly sequential frames.
func looksLikeRolls(frames map[string][]int) bool {
	shape := ""
	for key, fs := range frames {
		s := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return -1
			}
			return r
		}, strings.ToLower(key))
		if shape == "" {
			shape = s
		} else if s != shape {
			return false
		}
		if len(fs) < 3 {
			return false
		}
		slices.Sort(fs)
		steps := 0
		for i := 1; i < len(fs); i++ {
			if d := fs[i] - fs[i-1]; d == 0 || d == 1 {
				steps++
			}
		}
		if float64(steps) < 0.7*float64(len(fs)-1) {
			return false
		}
	}
	return true
}

func distinct(xs []string) int {
	seen := map[string]bool{}
	for _, x := range xs {
		seen[x] = true
	}
	return len(seen)
}

// genericFolder matches folder names that come from the download, not the roll ("OneDrive_2025-12-09").
var genericFolder = regexp.MustCompile(`(?i)^(onedrive|dropbox|we ?transfer|google ?drive|icloud|download(s)?|scans?|images?|photos?|pictures|export|untitled|new folder|order)([\s_.-]|$)|^[a-z]?\d+$`)

var dateInName = regexp.MustCompile(`(19|20)\d{2}[-_.]?(0[1-9]|1[0-2])[-_.]?(0[1-9]|[12]\d|3[01])`)

// folderDate finds a YYYY-MM-DD date in a folder name, or "".
func folderDate(name string) string {
	m := dateInName.FindString(name)
	digits := strings.NewReplacer("-", "", "_", "", ".", "").Replace(m)
	if len(digits) != 8 {
		return ""
	}
	return digits[:4] + "-" + digits[4:6] + "-" + digits[6:]
}

// labLabel describes a lab roll for people: "Order B001738, roll 1".
func labLabel(f labFrame) string { return fmt.Sprintf("Order %s, roll %d", f.Order, f.Roll) }
