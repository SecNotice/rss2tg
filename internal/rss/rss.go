package rss

import (
    "log"
    "strings"
    "sync"
    "time"

    "github.com/mmcdole/gofeed"
    "rss2telegram/internal/storage"
)

type MessageHandler func(title, url, group string, pubDate time.Time, matchedKeywords []string) error

type Manager struct {
    feeds          []*Feed
    db             *storage.Storage
    messageHandler MessageHandler
    mu             sync.Mutex
}

type Feed struct {
    URL      string
    Interval time.Duration
    Keywords []string
    Group    string
    ticker   *time.Ticker
    stopChan chan struct{}
}

type Config struct {
    URL      string
    Interval int
    Keywords []string
    Group    string
}

func NewManager(configs []Config, db *storage.Storage) *Manager {
    manager := &Manager{
        db: db,
    }
    manager.UpdateFeeds(configs)
    return manager
}

func (m *Manager) SetMessageHandler(handler MessageHandler) {
    m.messageHandler = handler
}

func (m *Manager) UpdateFeeds(configs []Config) {
    m.mu.Lock()
    defer m.mu.Unlock()

    // 停止所有现有的feed轮询器
    for _, feed := range m.feeds {
        if feed.stopChan != nil {
            close(feed.stopChan)
        }
    }

    // 创建新的feeds
    m.feeds = make([]*Feed, len(configs))
    for i, config := range configs {
        m.feeds[i] = &Feed{
            URL:      config.URL,
            Interval: time.Duration(config.Interval) * time.Second,
            Keywords: config.Keywords,
            Group:    config.Group,
            stopChan: make(chan struct{}),
        }
    }

    // 启动新的feed轮询器
    for _, feed := range m.feeds {
        go m.pollFeed(feed)
    }
}

func (m *Manager) Start() {
    log.Println("RSS管理器已启动")
    // 实际的轮询现在在UpdateFeeds中处理
}

func (m *Manager) pollFeed(feed *Feed) {
    feed.ticker = time.NewTicker(feed.Interval)
    defer feed.ticker.Stop()

    for {
        select {
        case <-feed.ticker.C:
            log.Printf("检查feed: %s", feed.URL)
            m.checkFeed(feed)
        case <-feed.stopChan:
            log.Printf("停止feed轮询器: %s", feed.URL)
            return
        }
    }
}

func (m *Manager) checkFeed(feed *Feed) {
    fp := gofeed.NewParser()
    parsedFeed, err := fp.ParseURL(feed.URL)
    if err != nil {
        log.Printf("解析Feed %s失败: %v", feed.URL, err)
        return
    }

    for _, item := range parsedFeed.Items {
        matchedKeywords := m.matchKeywords(item, feed.Keywords)
        if len(matchedKeywords) > 0 {
            log.Printf("发现新项目: %s", item.Title)
            if err := m.messageHandler(item.Title, item.Link, feed.Group, *item.PublishedParsed, matchedKeywords); err != nil {
                log.Printf("发送消息失败: %v", err)
            } else {
                log.Printf("成功发送项目的消息: %s", item.Title)
                m.db.MarkAsSent(item.Link)
            }
        }
    }
}

func (m *Manager) matchKeywords(item *gofeed.Item, keywords []string) []string {
    if m.db.WasSent(item.Link) {
        return nil
    }

    if len(keywords) == 0 {
        return []string{"无关键词"}
    }

    var matched []string
    for _, keyword := range keywords {
        if strings.Contains(strings.ToLower(item.Title), strings.ToLower(keyword)) || 
           strings.Contains(strings.ToLower(item.Description), strings.ToLower(keyword)) {
            matched = append(matched, keyword)
        }
    }

    return matched
}