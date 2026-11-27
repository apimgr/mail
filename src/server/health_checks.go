package server

import (
	"context"
	"sync"
	"time"

	"github.com/apimgr/mail/src/database"
	"github.com/apimgr/mail/src/paths"
)

type healthCheckCache struct {
	mu    sync.RWMutex
	value healthChecks
}

func newHealthCheckCache() *healthCheckCache {
	return &healthCheckCache{value: healthChecks{Database: "error", Cache: "error", Disk: "error", Scheduler: "error"}}
}

func (c *healthCheckCache) load() healthChecks {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.value
}

func (c *healthCheckCache) store(value healthChecks) {
	c.mu.Lock()
	c.value = value
	c.mu.Unlock()
}

func probeHealthChecks(db *database.DB, appPaths *paths.Paths, schedRunning bool) healthChecks {
	checks := healthChecks{Database: "error", Cache: "error", Disk: "error", Scheduler: "error"}
	if db != nil && db.ServerDB != nil && db.UsersDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		serverErr := db.ServerDB.PingContext(ctx)
		usersErr := error(nil)
		if db.UsersDB != db.ServerDB {
			usersErr = db.UsersDB.PingContext(ctx)
		}
		cancel()
		if serverErr == nil && usersErr == nil {
			checks.Database = "ok"
		}
	}
	if appPaths != nil {
		if accessibleDirectory(appPaths.Cache) {
			checks.Cache = "ok"
		}
		if accessibleDirectory(appPaths.DBDir) && accessibleDirectory(appPaths.Data) {
			checks.Disk = "ok"
		}
	}
	if schedRunning {
		checks.Scheduler = "ok"
	}
	return checks
}
