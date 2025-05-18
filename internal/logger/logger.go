package logger

import (
	"log"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.Logger
var mskLocation *time.Location

func init() {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		// Fallback or panic if timezone data is critical and not found
		// For simplicity, using UTC as a fallback with a warning.
		// In a production system where MSK is a hard requirement and tzdata is expected,
		// a panic might be more appropriate to signal a misconfigured environment.
		Logger.Warn("Failed to load Europe/Moscow location, using UTC for timestamps.", zap.Error(err))
		mskLocation = time.UTC
	} else {
		mskLocation = loc
	}
}

func customTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.In(mskLocation).Format("2006-01-02 15:04:05.000 MST"))
}

// Init initializes Logger.
// isDevelopment: flag for choosing log format (console/JSON) and level.
func Init(isDevelopment bool) {
	var cfg zap.Config
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = customTimeEncoder
	encoderCfg.EncodeCaller = zapcore.ShortCallerEncoder // Short caller format (package/file:line)

	if isDevelopment {
		cfg = zap.NewDevelopmentConfig() // Uses ConsoleEncoder, DebugLevel by default
		cfg.EncoderConfig = encoderCfg   // Apply our custom encoder settings (time, caller)
		cfg.OutputPaths = []string{"stdout"}
		cfg.ErrorOutputPaths = []string{"stderr"}
		// cfg.Level is already DebugLevel for NewDevelopmentConfig()
	} else { // Production
		cfg = zap.NewProductionConfig() // Uses JSONEncoder, InfoLevel by default
		cfg.EncoderConfig = encoderCfg  // Apply our custom encoder settings
		cfg.OutputPaths = []string{"stdout"}
		cfg.ErrorOutputPaths = []string{"stderr"}
	}

	var err error
	logger, err := cfg.Build(zap.AddCaller()) // AddCaller is good practice
	if err != nil {
		// Use standard log for this panic as our logger failed
		log.Panicf("failed to initialize zap logger: %v", err)
	}

	Logger = logger
	Logger.Info("Logger initialized",
		zap.Bool("developmentMode", isDevelopment),
		zap.String("logLevel", cfg.Level.Level().String()),
		zap.Strings("outputPaths", cfg.OutputPaths),
	)
}
