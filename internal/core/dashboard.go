package core

import "time"

// CustomDashboard represents a user-created custom dashboard
type CustomDashboard struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	WorkspaceID string         `json:"workspace_id"`
	Description string         `json:"description,omitempty"`
	Widgets     []WidgetConfig `json:"widgets"`
	RefreshSec  int            `json:"refresh_sec"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// WidgetConfig represents a single widget on a custom dashboard
type WidgetConfig struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Type       WidgetType        `json:"type"`
	Grid       WidgetGrid        `json:"grid"`
	Query      WidgetQuery       `json:"query"`
	Thresholds []WidgetThreshold `json:"thresholds,omitempty"`
}

// WidgetType identifies the visual widget type
type WidgetType string

const (
	WidgetLineChart WidgetType = "line_chart"
	WidgetBarChart  WidgetType = "bar_chart"
	WidgetGauge     WidgetType = "gauge"
	WidgetStat      WidgetType = "stat"
	WidgetTable     WidgetType = "table"
)

// WidgetGrid defines position and size on a 12-column grid
type WidgetGrid struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`  // 1-12
	Height int `json:"height"` // rows
}

// WidgetQuery defines the data source and filters for a widget
type WidgetQuery struct {
	Source      string            `json:"source"`
	Metric      string            `json:"metric"`
	Filters     map[string]string `json:"filters,omitempty"`
	TimeRange   string            `json:"time_range"`
	Aggregation string            `json:"aggregation,omitempty"`
}

// WidgetThreshold defines a visual threshold for coloring widget output
type WidgetThreshold struct {
	Value float64 `json:"value"`
	Color string  `json:"color"`
	Op    string  `json:"op"`
}
