package main

import (
	"errors"
	"sync"
)

type configuration struct {
	CursorAPIBaseURL string
}

func (p *Plugin) OnConfigurationChange() error {
	var next configuration
	if err := p.API.LoadPluginConfiguration(&next); err != nil {
		return err
	}
	p.setConfiguration(&next)
	return nil
}

func (p *Plugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()
	if p.configuration == nil {
		return &configuration{}
	}
	return p.configuration
}

func (p *Plugin) setConfiguration(next *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()
	if next == p.configuration {
		panic(errors.New("setConfiguration called with the existing configuration pointer"))
	}
	p.configuration = next
}

type configurationState struct {
	configurationLock sync.RWMutex
	configuration     *configuration
}
