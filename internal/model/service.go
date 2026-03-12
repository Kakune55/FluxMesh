package model

import (
	"fmt"
	"strings"
)

type ServiceConfig struct {
	Name      string         `json:"name"`
	Namespace string         `json:"namespace,omitempty"`
	Version   string         `json:"version,omitempty"`
	ResourceVersion int64    `json:"resource_version,omitempty"`
	Routes    []ServiceRoute `json:"routes"`
}

type ServiceRoute struct {
	PathPrefix  string `json:"path_prefix"`
	Destination string `json:"destination"`
	Weight      int    `json:"weight,omitempty"`
}

func (s ServiceConfig) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}

	if len(s.Routes) == 0 {
		return fmt.Errorf("at least one route is required")
	}

	for i, route := range s.Routes {
		if strings.TrimSpace(route.PathPrefix) == "" {
			return fmt.Errorf("routes[%d].path_prefix is required", i)
		}
		if strings.TrimSpace(route.Destination) == "" {
			return fmt.Errorf("routes[%d].destination is required", i)
		}
		if route.Weight < 0 || route.Weight > 100 {
			return fmt.Errorf("routes[%d].weight must be between 0 and 100", i)
		}
	}

	return nil
}
