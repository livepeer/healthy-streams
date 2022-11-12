package reducers

import (
	"time"

	"github.com/livepeer/livepeer-data/health"
)

var (
	statsWindows   = []time.Duration{1 * time.Minute, 10 * time.Minute, 1 * time.Hour}
	maxStatsWindow = statsWindows[len(statsWindows)-1]
)

func Default(golpExchange string, shardPrefixes []string, streamStateExchange string) health.Reducer {
	p := Pipeline{}
	if streamStateExchange != "" {
		p = append(p, StreamStateReducer{streamStateExchange})
	}
	return append(p, Pipeline{
		TranscodeReducer{golpExchange, shardPrefixes},
		MultistreamReducer{},
		MediaServerMetrics{},
		HealthReducer,
		StatsReducer(statsWindows),
	})
}

func DefaultStarTimeOffset() time.Duration {
	return maxStatsWindow
}
