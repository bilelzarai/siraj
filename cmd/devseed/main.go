// Sirāj development content: a catalog and a question bank to work against, so the
// admin console's eleven screens have rows on them while they are being built.
//
//	make dev-seed            # write it
//	make dev-seed clear=1    # take it away again
//
// This is a tool, not content the product ships with. No migration seeds a catalog
// (real domains, categories and questions are written by staff in the admin console),
// and nothing here runs from the server or the worker. Everything it writes is
// marked, and `clear=1` removes exactly what it wrote and nothing else.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"siraj/internal/config"
	"siraj/internal/db"
)

// seedTag is written into every slug this command creates, which is how `clear`
// knows what is its own and leaves anything a person entered alone.
const seedTag = "dev-"

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	clear := flag.Bool("clear", false, "remove the development content instead of writing it")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fail("config", err)
	}
	if !cfg.IsDev() {
		fail("env", fmt.Errorf("refusing to seed a production database"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		fail("db", err)
	}
	defer d.Close()

	if *clear {
		n, err := remove(ctx, d)
		if err != nil {
			fail("clear", err)
		}
		fmt.Printf("\n  removed %d development domains and everything under them.\n\n", n)
		return
	}
	counts, err := write(ctx, d)
	if err != nil {
		fail("seed", err)
	}
	fmt.Printf("\n  %d domains, %d categories, %d questions.\n  Open %s/admin\n\n",
		counts.domains, counts.categories, counts.questions, cfg.BaseURL)
}

type counts struct{ domains, categories, questions int }

// domain is one line of the catalog, with the three names a player can read.
type domain struct {
	slug, family, icon, color, motif string
	ar, en, fr                       string
	categories                       []category
}

type category struct {
	slug, icon string
	ar, en, fr string
	retired    bool
}

// catalog is the shape the design draws: two Islamic domains first, then the
// general ones, each with the categories its screens show.
var catalog = []domain{
	{"islamic-knowledge", "islamic", "mosque", "#0b7a5c", "mosque",
		"المعرفة الإسلامية", "Islamic Knowledge", "Savoir islamique", []category{
			{"the-noble-quran", "book", "القرآن الكريم", "The Noble Qur'an", "Le Noble Coran", false},
			{"prophetic-biography", "scroll", "السيرة النبوية", "Prophetic Biography", "Biographie du Prophète", false},
			{"stories-of-the-prophets", "feather", "قصص الأنبياء", "Stories of the Prophets", "Histoires des prophètes", false},
			{"fiqh-worship", "scale", "الفقه والعبادات", "Fiqh & Worship", "Fiqh et adoration", false},
			{"hadith", "book", "الحديث", "Hadith", "Hadith", false},
			{"islamic-history", "columns", "التاريخ الإسلامي", "Islamic History", "Histoire islamique", false},
			{"ramadan-fasting", "moon", "رمضان والصيام", "Ramadan & Fasting", "Ramadan et jeûne", false},
			{"akhlaq-manners", "heart", "الأخلاق والآداب", "Akhlaq & Manners", "Akhlaq et bonnes manières", false},
		}},
	{"arabic-quranic", "islamic", "scroll", "#8a5cf6", "arabesque",
		"العربية وعلوم القرآن", "Arabic & Qur'anic Sciences", "Arabe et sciences coraniques", []category{
			{"grammar-nahw", "type", "النحو", "Grammar (Naḥw)", "Grammaire (Naḥw)", false},
			{"vocabulary", "languages", "المفردات", "Vocabulary", "Vocabulaire", false},
			{"morphology-sarf", "puzzle", "الصرف", "Morphology (Ṣarf)", "Morphologie (Ṣarf)", false},
			{"eloquence-balagha", "feather", "البلاغة", "Eloquence (Balāgha)", "Éloquence (Balāgha)", false},
		}},
	{"sport", "general", "ball", "#c2410c", "none",
		"الرياضة", "Sport", "Sport", []category{
			{"football", "ball", "كرة القدم", "Football", "Football", false},
			{"olympic-games", "trophy", "الألعاب الأولمبية", "Olympic Games", "Jeux olympiques", false},
			{"world-sports", "globe", "رياضات العالم", "World sports", "Sports du monde", false},
		}},
	{"geography", "general", "map", "#2563eb", "none",
		"الجغرافيا", "Geography", "Géographie", []category{
			{"capitals", "map", "العواصم", "Capitals", "Capitales", false},
			{"rivers-deserts-oceans", "globe", "الأنهار والصحاري والمحيطات", "Rivers, deserts & oceans", "Fleuves, déserts et océans", false},
		}},
	{"business", "general", "briefcase", "#b45309", "none",
		"الأعمال والاقتصاد", "Business & Economy", "Affaires et économie", []category{
			{"economics", "chart", "الاقتصاد", "Economics", "Économie", false},
			{"money-currencies", "coin", "النقود والعملات", "Money & currencies", "Monnaie et devises", false},
			{"companies-work", "briefcase", "الشركات والعمل", "Companies & work", "Entreprises et travail", false},
		}},
	{"science", "general", "atom", "#0891b2", "none",
		"العلوم", "Science", "Sciences", []category{
			{"space", "atom", "الفضاء", "Space", "Espace", false},
			{"human-body", "heart", "جسم الإنسان", "Human body", "Corps humain", false},
			// One retired category, so the "Retired 1" filter has its row.
			{"chemistry", "flask", "الكيمياء", "Chemistry", "Chimie", true},
		}},
}

// statuses cycles the four a question can be in, so every state filter of A06 has
// something to show.
var statuses = []string{"published", "published", "published", "review", "draft", "archived"}

func write(ctx context.Context, d *db.DB) (counts, error) {
	var out counts
	err := d.Pool.Ping(ctx)
	if err != nil {
		return out, err
	}
	for i, dom := range catalog {
		var domainID int64
		err := d.Pool.QueryRow(ctx, `
			INSERT INTO domains (slug, family, icon, color, motif, position, is_featured)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (slug) DO UPDATE SET icon = EXCLUDED.icon
			RETURNING id`,
			seedTag+dom.slug, dom.family, dom.icon, dom.color, dom.motif, i+1, dom.family == "islamic",
		).Scan(&domainID)
		if err != nil {
			return out, fmt.Errorf("domain %s: %w", dom.slug, err)
		}
		out.domains++
		if err := text3(ctx, d, `
			INSERT INTO domain_translations (domain_id, lang, name, description)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (domain_id, lang) DO UPDATE SET name = EXCLUDED.name`,
			domainID, dom.ar, dom.en, dom.fr, true); err != nil {
			return out, err
		}

		for j, cat := range dom.categories {
			var categoryID int64
			err := d.Pool.QueryRow(ctx, `
				INSERT INTO categories (domain_id, slug, icon, color, position, is_published)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (domain_id, slug) DO UPDATE SET icon = EXCLUDED.icon
				RETURNING id`,
				domainID, seedTag+cat.slug, cat.icon, dom.color, j+1, !cat.retired,
			).Scan(&categoryID)
			if err != nil {
				return out, fmt.Errorf("category %s: %w", cat.slug, err)
			}
			out.categories++
			if err := text3(ctx, d, `
				INSERT INTO category_translations (category_id, lang, name)
				VALUES ($1, $2, $3)
				ON CONFLICT (category_id, lang) DO UPDATE SET name = EXCLUDED.name`,
				categoryID, cat.ar, cat.en, cat.fr, false); err != nil {
				return out, err
			}

			// Four questions per category, walking the states and the three levels.
			for k := 0; k < 4; k++ {
				n := out.questions
				status := statuses[n%len(statuses)]
				level := 1 + (n % 3)
				var questionID int64
				err := d.Pool.QueryRow(ctx, `
					INSERT INTO questions (category_id, difficulty, points, correct_index, status, main_lang, published_at)
					VALUES ($1, $2, $3, $4, $5::question_status, 'en',
				        CASE WHEN $5::question_status = 'published' THEN now() END)
					RETURNING id`,
					categoryID, level, 10*level, n%4, status,
				).Scan(&questionID)
				if err != nil {
					return out, fmt.Errorf("question in %s: %w", cat.slug, err)
				}
				out.questions++
				// Every seventh question is missing its French, so the
				// "Missing a language" filter of A06 has rows.
				for _, lang := range []string{"ar", "en", "fr"} {
					if lang == "fr" && n%7 == 0 {
						continue
					}
					_, err := d.Pool.Exec(ctx, `
						INSERT INTO question_translations (question_id, lang, body, answers, explanation)
						VALUES ($1, $2, $3, $4, $5)
						ON CONFLICT (question_id, lang) DO NOTHING`,
						questionID, lang,
						fmt.Sprintf("%s · sample question %d (%s)?", cat.en, n+1, lang),
						[]string{"Answer A", "Answer B", "Answer C", "Answer D"},
						"A sample explanation, so the review screen has something to read.")
					if err != nil {
						return out, fmt.Errorf("question text: %w", err)
					}
				}
			}
		}
	}
	return out, nil
}

// text3 writes one row per language, for a table whose rows are (id, lang, name…).
func text3(ctx context.Context, d *db.DB, query string, id int64, ar, en, fr string, withDescription bool) error {
	for lang, name := range map[string]string{"ar": ar, "en": en, "fr": fr} {
		args := []any{id, lang, name}
		if withDescription {
			args = append(args, name+" — development content.")
		}
		if _, err := d.Pool.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("%s text: %w", lang, err)
		}
	}
	return nil
}

// remove deletes the domains this command wrote. Categories, questions and their
// translations go with them through the schema's own cascades, except questions,
// which the categories hold back — so they go first.
func remove(ctx context.Context, d *db.DB) (int, error) {
	if _, err := d.Pool.Exec(ctx, `
		DELETE FROM questions q USING categories c, domains dm
		WHERE q.category_id = c.id AND c.domain_id = dm.id AND dm.slug LIKE $1`, seedTag+"%"); err != nil {
		return 0, err
	}
	if _, err := d.Pool.Exec(ctx, `
		DELETE FROM categories c USING domains dm
		WHERE c.domain_id = dm.id AND dm.slug LIKE $1`, seedTag+"%"); err != nil {
		return 0, err
	}
	tag, err := d.Pool.Exec(ctx, `DELETE FROM domains WHERE slug LIKE $1`, seedTag+"%")
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func fail(what string, err error) {
	fmt.Fprintf(os.Stderr, "\n  %s: %v\n\n", what, strings.TrimSpace(err.Error()))
	os.Exit(1)
}
