package cli

import (
	"context"
	"os"
	"sync"

	"github.com/fatih/color"
)

// styleKey is the context key for storing Style.
type styleKey struct{}

// Style provides a centralized styling palette for terminal output.
// It handles NO_COLOR environment variable and --no-color flag uniformly.
type Style struct {
	// Core colors
	Cyan       *color.Color
	CyanBold   *color.Color
	Green      *color.Color
	GreenBold  *color.Color
	Yellow     *color.Color
	YellowBold *color.Color
	Red        *color.Color
	RedBold    *color.Color
	Magenta    *color.Color
	Dim        *color.Color
	Bold       *color.Color

	// Whether color is disabled
	NoColor bool
}

// Terminal symbols for visual feedback.
const (
	SymbolSuccess = "✓"
	SymbolError   = "✗"
	SymbolWarning = "⚠"
	SymbolInfo    = "ℹ"
	SymbolArrow   = "→"
	SymbolBullet  = "•"
	SymbolDebug   = "●"
)

var (
	// defaultStyle is the singleton style instance, initialized lazily.
	defaultStyle *Style
	styleOnce    sync.Once
	styleMu      sync.RWMutex
)

// InitStyle initializes the global styling with the given noColor flag.
// This should be called once during CLI initialization, typically in PersistentPreRunE.
// It checks both the noColor flag and the NO_COLOR environment variable.
func InitStyle(noColor bool) {
	styleMu.Lock()
	defer styleMu.Unlock()

	// Determine if color should be disabled
	disabled := noColor || os.Getenv("NO_COLOR") != ""

	// Set the global color.NoColor flag
	color.NoColor = disabled

	// Create the style instance
	defaultStyle = newStyle(disabled)
}

// GetStyle returns the global Style instance.
// If InitStyle has not been called, it initializes with default settings
// (checking only the NO_COLOR environment variable).
//
// Deprecated: Prefer StyleFromContext(ctx) when context is available.
// This function remains for backward compatibility and for utility functions
// that don't have access to context.
func GetStyle() *Style {
	styleMu.RLock()
	if defaultStyle != nil {
		s := defaultStyle
		styleMu.RUnlock()
		return s
	}
	styleMu.RUnlock()

	// Initialize with defaults if not already initialized
	styleOnce.Do(func() {
		InitStyle(false)
	})

	styleMu.RLock()
	defer styleMu.RUnlock()
	return defaultStyle
}

// IsNoColor returns whether color output is disabled.
// This checks both the initialized state and the NO_COLOR environment variable.
func IsNoColor() bool {
	return GetStyle().NoColor
}

// newStyle creates a new Style with the given noColor setting.
func newStyle(noColor bool) *Style {
	return &Style{
		Cyan:       color.New(color.FgCyan),
		CyanBold:   color.New(color.FgCyan, color.Bold),
		Green:      color.New(color.FgGreen),
		GreenBold:  color.New(color.FgGreen, color.Bold),
		Yellow:     color.New(color.FgYellow),
		YellowBold: color.New(color.FgYellow, color.Bold),
		Red:        color.New(color.FgRed),
		RedBold:    color.New(color.FgRed, color.Bold),
		Magenta:    color.New(color.FgMagenta),
		Dim:        color.New(color.Faint),
		Bold:       color.New(color.Bold),
		NoColor:    noColor,
	}
}

// NewStyle creates a new Style instance with the given noColor setting.
// This is useful for testing or when you need an isolated style instance.
func NewStyle(noColor bool) *Style {
	// Also update the global color.NoColor flag for consistency
	if noColor {
		color.NoColor = true
	}
	return newStyle(noColor)
}

// WithStyle returns a new context with the Style attached.
// Use this to pass style through the command execution chain.
func WithStyle(ctx context.Context, s *Style) context.Context {
	return context.WithValue(ctx, styleKey{}, s)
}

// StyleFromContext extracts the Style from context.
// Returns a default style (with color enabled) if not present.
func StyleFromContext(ctx context.Context) *Style {
	if s, ok := ctx.Value(styleKey{}).(*Style); ok && s != nil {
		return s
	}
	// Fallback to global style for backward compatibility
	return GetStyle()
}
