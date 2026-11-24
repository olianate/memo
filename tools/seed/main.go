// Command seed fills a development profile with representative memos so the
// UI, statistics screen and smoke checklist have something to show.
//
//	MEMO_DATA_DIR=%TEMP%\memo-demo go run ./tools/seed
//	MEMO_DATA_DIR=%TEMP%\memo-demo ./bin/memo.exe
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/olianate/memo/internal/config"
	"github.com/olianate/memo/internal/domain"
	"github.com/olianate/memo/internal/domain/markdown"
	"github.com/olianate/memo/internal/platform"
	"github.com/olianate/memo/internal/store"
)

func block(t domain.BlockType, text string, extra func(*domain.Block)) domain.Block {
	b := domain.Block{ID: domain.NewID(), Type: t, Spans: []domain.Span{{Text: text}}}
	if extra != nil {
		extra(&b)
	}
	return b
}

func main() {
	dir, err := platform.EnsureDataDir()
	if err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "memo.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	cfg := config.New(st)
	_ = cfg.SetBool(ctx, config.KeyOnboarded, true)
	_ = cfg.SetBool(ctx, config.KeySyncEnabled, false)
	_ = cfg.Set(ctx, config.KeyTheme, "light")
	device := cfg.Get(ctx, config.KeyDeviceID)
	if device == "" {
		device = domain.NewID()
		_ = cfg.Set(ctx, config.KeyDeviceID, device)
	}
	_, _ = st.EnsureDevice(ctx, device)

	notes, _, _, _ := st.Counts(ctx)
	if notes > 0 {
		fmt.Printf("seed: %s already has %d memos; nothing to do\n", dir, notes)
		return
	}

	now := time.Now()
	at := func(daysAgo, hour int) time.Time {
		d := now.AddDate(0, 0, -daysAgo)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, 12, 0, 0, time.Local)
	}
	seed := []struct {
		at     time.Time
		tags   []string
		blocks []domain.Block
	}{
		{
			at:   at(0, 9),
			tags: []string{"产品", "memo"},
			blocks: []domain.Block{
				block(domain.BlockParagraph, "现在的想法是… 支持 Markdown 的实时渲染与源码模式，标签在正文之外管理。", nil),
				block(domain.BlockBullet, "Enter 保存，Shift+Enter 换行", nil),
				block(domain.BlockTodo, "把 sticky window 做成全局快捷键", func(b *domain.Block) { b.Checked = true }),
			},
		},
		{
			at:   at(0, 8),
			tags: []string{"产品", "memo", "技术"},
			blocks: []domain.Block{
				block(domain.BlockHeading, "把 Notion 当归档，而不是实时数据库", func(b *domain.Block) { b.Level = 2 }),
				block(domain.BlockParagraph, "搜索必须留在本地。Notion 只负责长期归档与跨设备同步。", nil),
				block(domain.BlockParagraph, "本地保留 30 天后，正文和附件会被清理，但元数据、标签和搜索索引仍在。", nil),
			},
		},
		{
			at:   at(1, 21),
			tags: []string{"技术", "todo"},
			blocks: []domain.Block{
				block(domain.BlockParagraph, "Wails 的 IME 问题用 PR #6596 修好了。", nil),
				block(domain.BlockParagraph, "Windows 11 + 微软拼音通过。macOS 留到发布前再补测。", nil),
			},
		},
		{
			at:   at(1, 16),
			tags: []string{"产品", "设计"},
			blocks: []domain.Block{
				block(domain.BlockParagraph, "给 sticky window 加一个「保留本地」的 pin。", nil),
				block(domain.BlockParagraph, "清理正文时不要动它。", nil),
			},
		},
		{
			at:   at(3, 22),
			tags: []string{"生活", "memo"},
			blocks: []domain.Block{
				block(domain.BlockHeading, "本周回顾", func(b *domain.Block) { b.Level = 1 }),
				block(domain.BlockParagraph, "慢一点，但每天都在写。", nil),
				block(domain.BlockQuote, "记录本身就是一种整理。", nil),
			},
		},
		{
			at:   at(5, 10),
			tags: []string{"技术", "memo"},
			blocks: []domain.Block{
				block(domain.BlockParagraph, "中文搜索要支持 1–2 个字。FTS5 trigram 不够，短查询回退到 LIKE。", nil),
				block(domain.BlockCode, "SELECT * FROM memos WHERE body_text LIKE '%开会%'", func(b *domain.Block) { b.Language = "sql" }),
			},
		},
	}
	for _, item := range seed {
		if _, err := st.CreateMemo(ctx, store.MemoInput{
			CreatedAt: item.at, UpdatedAt: item.at, Body: markdown.Serialize(item.blocks), Tags: item.tags,
		}); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("seed: wrote %d memos to %s\n", len(seed), dir)
	_ = os.Stdout.Sync()
}
