package logger

import (
	"log"
	"os"
)

type Logger struct {
	prefix string
	logger *log.Logger
}

func New(service string) *Logger {
	return &Logger{
		prefix: "[" + service + "] ",
		logger: log.New(os.Stdout, "", log.LstdFlags),
	}
}

func (l *Logger) Printf(format string, v ...interface{}) {
	l.logger.Printf(l.prefix+format, v...)
}

func (l *Logger) Print(v ...interface{}) {
	args := make([]interface{}, 0, len(v)+1)
	args = append(args, l.prefix)
	args = append(args, v...)
	l.logger.Print(args...)
}

func (l *Logger) Println(v ...interface{}) {
	args := make([]interface{}, 0, len(v)+1)
	args = append(args, l.prefix)
	args = append(args, v...)
	l.logger.Println(args...)
}

func (l *Logger) Fatal(v ...interface{}) {
	args := make([]interface{}, 0, len(v)+1)
	args = append(args, l.prefix)
	args = append(args, v...)
	l.logger.Fatal(args...)
}

func (l *Logger) Fatalf(format string, v ...interface{}) {
	l.logger.Fatalf(l.prefix+format, v...)
}
