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
	if layout == nil {
		return nil, errors.New("need a layout")
	}

	rombo := Rombo{
		datafile:    datafile,
		destructive: destructive,
		layout:      layout,
		logger:      logger,
	}
	return &rombo, nil
}
