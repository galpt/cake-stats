package log

import (
	"os"

	"github.com/rs/zerolog"
)

// Global logger instance.  Other packages should use log.Logger with
// additional context fields rather than importing zerolog directly.
var Logger zerolog.Logger

func init() {
	Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
}
