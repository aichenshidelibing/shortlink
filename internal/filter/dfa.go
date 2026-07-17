package filter

import (
	"sync"
	"unicode"
)

type Node struct {
	Children map[rune]*Node
	IsEnd    bool
	Level    int
}

type DFA struct {
	mu       sync.RWMutex
	root     *Node
	wordMap  map[string]int
	whiteList []string
	whiteRegex []string
}

func NewDFA() *DFA {
	return &DFA{
		root:    &Node{Children: make(map[rune]*Node)},
		wordMap: make(map[string]int),
	}
}

func (d *DFA) AddWord(word string, level int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	node := d.root
	for _, r := range word {
		if node.Children == nil {
			node.Children = make(map[rune]*Node)
		}
		if _, ok := node.Children[r]; !ok {
			node.Children[r] = &Node{Children: make(map[rune]*Node)}
		}
		node = node.Children[r]
	}
	node.IsEnd = true
	node.Level = level
	d.wordMap[word] = level
}

func (d *DFA) RemoveWord(word string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.wordMap, word)
	d.rebuild()
}

func (d *DFA) rebuild() {
	d.root = &Node{Children: make(map[rune]*Node)}
	for word, level := range d.wordMap {
		node := d.root
		for _, r := range word {
			if node.Children == nil {
				node.Children = make(map[rune]*Node)
			}
			if _, ok := node.Children[r]; !ok {
				node.Children[r] = &Node{Children: make(map[rune]*Node)}
			}
			node = node.Children[r]
		}
		node.IsEnd = true
		node.Level = level
	}
}

func (d *DFA) ReloadWords(words map[string]int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.wordMap = words
	d.rebuild()
}

func (d *DFA) SetWhiteList(exact []string, regex []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.whiteList = exact
	d.whiteRegex = regex
}

func (d *DFA) Check(text string) (found bool, level int, match string) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		node := d.root
		for j := i; j < len(runes); j++ {
			r := runes[j]
			if node.Children == nil {
				break
			}
			if next, ok := node.Children[r]; ok {
				node = next
				if node.IsEnd {
					candidate := string(runes[i : j+1])
					if !d.inWhiteList(candidate) {
						return true, node.Level, candidate
					}
				}
			} else {
				break
			}
		}
	}
	return false, 0, ""
}

func (d *DFA) inWhiteList(word string) bool {
	for _, w := range d.whiteList {
		if w == word {
			return true
		}
	}
	return false
}

func NormalizeText(text string) string {
	var sb []rune
	for _, r := range text {
		switch {
		case r >= 'A' && r <= 'Z':
			sb = append(sb, unicode.ToLower(r))
		case r >= 'a' && r <= 'z':
			sb = append(sb, r)
		case r >= '0' && r <= '9':
			sb = append(sb, r)
		case r >= '一' && r <= '鿿':
			sb = append(sb, r)
		default:
			sb = append(sb, r)
		}
	}
	return string(sb)
}
