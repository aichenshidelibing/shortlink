package service

import (
	"bufio"
	"context"
	"os"
	"shortlink/internal/filter"
	"shortlink/internal/model"
	"shortlink/internal/repository"
	"strings"

	"go.uber.org/zap"
)

type WordFilterService struct {
	repo *repository.WordRepository
	dfa  *filter.DFA
	log  *zap.Logger
}

func NewWordFilterService(repo *repository.WordRepository, dfa *filter.DFA, log *zap.Logger) *WordFilterService {
	return &WordFilterService{repo: repo, dfa: dfa, log: log}
}

func (s *WordFilterService) Reload(ctx context.Context) error {
	rules, err := s.repo.ListRules(ctx)
	if err != nil {
		return err
	}

	words := make(map[string]int)
	for _, r := range rules {
		if r.Enabled {
			words[r.Word] = r.Level
		}
	}

	whites, err := s.repo.ListWhiteList(ctx)
	if err != nil {
		return err
	}

	var exact []string
	var regex []string
	for _, w := range whites {
		if w.Type == "word" {
			if w.IsRegex {
				regex = append(regex, w.Pattern)
			} else {
				exact = append(exact, w.Pattern)
			}
		}
	}

	s.dfa.ReloadWords(words)
	s.dfa.SetWhiteList(exact, regex)
	return nil
}

// SeedFromFile loads words from a text file (one word per line) into the DB and DFA.
// Lines starting with # are treated as comments and skipped.
// Returns the number of new words inserted.
func (s *WordFilterService) SeedFromFile(ctx context.Context, filePath string, defaultLevel int) (int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	// Load existing words to avoid duplicates
	existing, _ := s.repo.ListRules(ctx)
	have := make(map[string]bool)
	for _, r := range existing {
		have[r.Word] = true
	}

	var batch []model.WordRule
	count := 0

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 128*1024), 128*1024) // handle long lines

	for scanner.Scan() {
		word := strings.TrimSpace(scanner.Text())
		if word == "" || strings.HasPrefix(word, "#") {
			continue
		}
		// Skip very short words (too many false positives)
		if len([]rune(word)) < 2 {
			continue
		}
		if have[word] {
			continue
		}
		have[word] = true

		// Assign level based on word characteristics
		level := defaultLevel
		if level == 0 {
			level = 1
		}

		batch = append(batch, model.WordRule{Word: word, Level: level, Enabled: true})
		count++

		// Batch insert every 500 words
		if len(batch) >= 500 {
			if err := s.repo.BatchCreate(ctx, batch); err != nil {
				s.log.Warn("seed words batch failed", zap.Error(err))
			}
			batch = nil
		}
	}

	// Final batch
	if len(batch) > 0 {
		if err := s.repo.BatchCreate(ctx, batch); err != nil {
			s.log.Warn("seed words final batch failed", zap.Error(err))
		}
	}

	if err := scanner.Err(); err != nil {
		return count, err
	}

	// Reload DFA with new words
	s.Reload(ctx)

	s.log.Info("seeded words from file", zap.Int("new", count), zap.String("file", filePath))
	return count, nil
}

func (s *WordFilterService) Check(text string) (found bool, level int, match string) {
	normalized := filter.NormalizeText(text)
	return s.dfa.Check(normalized)
}
