package logger

import (
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger - глобальный экземпляр zap.Logger
var Logger *zap.Logger

// mskLocation хранит информацию о московском часовом поясе.
var mskLocation *time.Location

func init() {
	// Загружаем информацию о часовом поясе МСК (UTC+3) один раз при инициализации пакета.
	// Важно: На системе, где будет запускаться приложение, должны быть данные о часовых поясах (tzdata).
	// В Docker-образе ubuntu:24.04 они обычно есть.
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		// Если не удалось загрузить МСК, логи будут в UTC или по умолчанию для системы.
		// Выводим панику, так как это важное требование.
		// Либо можно использовать time.FixedZone("MSK", 3*60*60) если tzdata нет,
		// но LoadLocation предпочтительнее, так как учитывает летнее/зимнее время (если применимо).
		// Россия сейчас не переходит на летнее время, так что FixedZone тоже вариант.
		panic("failed to load Europe/Moscow location: " + err.Error())
	}
	mskLocation = loc
}

// customTimeEncoder форматирует время для логов в МСК.
func customTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.In(mskLocation).Format("2006-01-02 15:04:05.000 MST"))
	// Формат можно изменить, например: "2006-01-02T15:04:05.000Z07:00" для ISO8601 с таймзоной
	// или "02.01.2006 15:04:05"
}

// Init инициализирует Logger.
// logFilePath: путь к файлу для логов (например, "logs/app.log").
// isDevelopment: флаг, указывающий, используется ли конфигурация для разработки (более читаемый вывод).
//
//	Если false, используется production-конфигурация (JSON).
func Init(logFilePath string, isDevelopment bool) {
	var cfg zap.Config
	encoderCfg := zap.NewProductionEncoderConfig()

	// Настраиваем формат времени
	encoderCfg.EncodeTime = customTimeEncoder // Наш кастомный форматтер времени в МСК

	// Настраиваем формат для stacktrace, чтобы он был более читаемым (особенно для ConsoleEncoder)
	encoderCfg.EncodeCaller = zapcore.ShortCallerEncoder // Более короткий формат вызывающего (package/file:line)

	if isDevelopment {
		cfg = zap.NewDevelopmentConfig()
		cfg.EncoderConfig = encoderCfg
		cfg.OutputPaths = []string{"stdout"} // Только stdout для разработки
		cfg.ErrorOutputPaths = []string{"stderr"}
		cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	} else { // Production
		cfg = zap.NewProductionConfig()
		cfg.EncoderConfig = encoderCfg
		cfg.OutputPaths = []string{"stdout"} // Только stdout для JSON логов в продакшене
		cfg.ErrorOutputPaths = []string{"stderr"}
		// Уровень INFO по умолчанию для ProductionConfig
	}

	// Добавляем опцию вывода информации о вызывающем (caller)
	// Для Production это уже включено в NewProductionConfig, для Development - тоже.
	// Но если мы строим конфиг с нуля, то zap.AddCaller() нужен.
	// В данном случае cfg.Build() уже будет с AddCaller, так как он есть в NewProductionConfig/NewDevelopmentConfig
	var err error
	logger, err := cfg.Build(zap.AddCaller()) // zap.AddCaller() здесь для явности, хотя конфиги его уже могут включать
	if err != nil {
		panic("failed to initialize zap logger: " + err.Error())
	}

	Logger = logger
	Logger.Info("Logger initialized",
		zap.Bool("developmentMode", isDevelopment),
		zap.String("logLevel", cfg.Level.Level().String()),
		zap.Strings("outputPaths", cfg.OutputPaths), // Будет показывать [stdout]
	)
}
