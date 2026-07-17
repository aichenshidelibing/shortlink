package hashids

import (
	"strings"
)

const (
	alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

type Hashids struct {
	salt      string
	alphabet  string
	seps      string
	guards    string
	minLength int
}

func New(salt string, minLength int) *Hashids {
	h := &Hashids{
		salt:      salt,
		alphabet:  alphabet,
		minLength: minLength,
	}
	h.setup()
	return h
}

func (h *Hashids) setup() {
	uniqueAlphabet := make([]rune, 0, len(h.alphabet))
	seen := make(map[rune]bool)
	for _, r := range h.alphabet {
		if !seen[r] {
			seen[r] = true
			uniqueAlphabet = append(uniqueAlphabet, r)
		}
	}
	h.alphabet = string(uniqueAlphabet)

	seps := "cfhistuCFHISTU"
	var sepsSlice []rune
	for _, r := range seps {
		if strings.ContainsRune(h.alphabet, r) {
			sepsSlice = append(sepsSlice, r)
		}
	}
	h.seps = string(sepsSlice)

	for _, r := range h.seps {
		h.alphabet = strings.Replace(h.alphabet, string(r), "", 1)
	}

	if len(h.seps) == 0 {
		mid := len(h.alphabet) / 2
		h.seps = h.alphabet[mid : mid+1]
		h.alphabet = h.alphabet[:mid] + h.alphabet[mid+1:]
	}

	h.seps = h.consistentShuffle(h.seps, h.salt)

	saltLen := len([]rune(h.salt))
	if saltLen > 0 {
		sepsLen := len([]rune(h.seps))
		alphabetLen := len([]rune(h.alphabet))
		if sepsLen >= alphabetLen/3 {
			sepsLen = alphabetLen / 3
			sepsRunes := []rune(h.seps)
			h.seps = string(sepsRunes[:sepsLen])
		}
	}

	h.alphabet = h.consistentShuffle(h.alphabet, h.salt)

	guardCount := len([]rune(h.alphabet)) / 12
	if guardCount > 3 {
		guardCount = 3
	}
	alphabetRunes := []rune(h.alphabet)
	h.guards = string(alphabetRunes[:guardCount])
	h.alphabet = string(alphabetRunes[guardCount:])
}

func (h *Hashids) consistentShuffle(str, salt string) string {
	if salt == "" {
		return str
	}
	runes := []rune(str)
	saltRunes := []rune(salt)
	lenStr := len(runes)
	lenSalt := len(saltRunes)
	p := 0

	for i := lenStr - 1; i > 0; i-- {
		p += int(saltRunes[i%lenSalt])
		j := (int(saltRunes[i%lenSalt]) + i + p) % i
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func (h *Hashids) Encode(numbers []int64) string {
	if len(numbers) == 0 {
		return ""
	}

	alphabet := h.alphabet
	numbersHash := int64(0)
	for i, n := range numbers {
		numbersHash += n % int64(i+100)
	}

	lottery := []rune(alphabet)[numbersHash%int64(len([]rune(alphabet)))]
	result := string(lottery)

	alphabetRunes := []rune(alphabet)
	for i, n := range numbers {
		salt := string(lottery) + h.salt + string(alphabetRunes[i%len(alphabetRunes)])
		alphabet = h.consistentShuffle(alphabet, salt)
		last := h.hash(n, alphabet)
		result += last
		if i < len(numbers)-1 {
			n %= int64(len([]rune(last)))
			sepsIdx := n % int64(len([]rune(h.seps)))
			result += string([]rune(h.seps)[sepsIdx])
		}
	}

	if len(result) < h.minLength {
		guardIdx := (numbersHash + int64([]rune(result)[0])) % int64(len([]rune(h.guards)))
		guard := string([]rune(h.guards)[guardIdx])
		result = guard + result
		if len(result) < h.minLength {
			guardIdx = (numbersHash + int64([]rune(result)[2])) % int64(len([]rune(h.guards)))
			guard = string([]rune(h.guards)[guardIdx])
			result += guard
		}
	}

	halfLen := len([]rune(alphabet)) / 2
	for len([]rune(result)) < h.minLength {
		alphabet = h.consistentShuffle(alphabet, alphabet)
		result = string([]rune(alphabet)[halfLen:]) + result + string([]rune(alphabet)[:halfLen])
		excess := len([]rune(result)) - h.minLength
		if excess > 0 {
			start := excess / 2
			result = string([]rune(result)[start : start+h.minLength])
		}
	}

	return result
}

func (h *Hashids) EncodeOne(number int64) string {
	return h.Encode([]int64{number})
}

func (h *Hashids) hash(number int64, alphabet string) string {
	result := ""
	runes := []rune(alphabet)
	alphabetLen := int64(len(runes))
	for {
		result = string(runes[number%alphabetLen]) + result
		number = number / alphabetLen
		if number == 0 {
			break
		}
	}
	return result
}
