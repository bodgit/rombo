package rombo

import (
	"errors"
	"log"
)

type Rombo struct {
	datafile    *Datafile
	destructive bool
	layout      Layout
	logger      *log.Logger
}

func New(datafile *Datafile, logger *log.Logger, destructive bool, layout Layout) (*Rombo, error) {
	if datafile == nil {
		return nil, errors.New("need a database")
	}
	if logger == nil {
		return nil, errors.New("need a logger")
	}

	l := layout
	if layout == nil {
		l = SimpleCompressed{}
	}

	rombo := Rombo{
		datafile:    datafile,
		destructive: destructive,
		layout:      l,
		logger:      logger,
	}
	return &rombo, nil
}
