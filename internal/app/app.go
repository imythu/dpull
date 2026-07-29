package app

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/imythu/dpull/internal/cache"
	"github.com/imythu/dpull/internal/cleanup"
	"github.com/imythu/dpull/internal/compose"
	"github.com/imythu/dpull/internal/crane"
	"github.com/imythu/dpull/internal/docker"
	"github.com/imythu/dpull/internal/logger"
	"github.com/imythu/dpull/internal/util"
)

type Options struct {
	Images      []string
	ComposeFile string
	Proxy       string
	Keep        bool
	Up          bool
}

type Application struct {
	Cache          *cache.Manager
	Crane          crane.Client
	Docker         docker.Client
	Log            *logger.Logger
	Now            func() time.Time
	CraneInstaller interface {
		Ensure(context.Context, string) (string, error)
	}
}

type Result struct {
	Total   int
	Success int
	Failed  int
}

func (a *Application) Clean() (int, error) {
	if err := a.Cache.CreateRoot(); err != nil {
		return 0, fmt.Errorf("prepare cache for cleanup: %w", err)
	}
	removed, err := cleanup.All(a.Cache.Root)
	if err != nil {
		return removed, err
	}
	return removed, nil
}

func (a *Application) Run(ctx context.Context, options Options) error {
	started := a.Now()
	if a.CraneInstaller != nil {
		binary, err := a.CraneInstaller.Ensure(ctx, options.Proxy)
		if err != nil {
			return fmt.Errorf("ensure crane is installed: %w", err)
		}
		a.Crane.Binary = binary
	}
	a.Log.Banner()
	if err := a.initialize(); err != nil {
		return err
	}
	images, composeFile, err := a.resolveImages(options)
	if err != nil {
		return err
	}
	runDir, err := a.Cache.CreateRunDir()
	if err != nil {
		return fmt.Errorf("prepare run cache: %w", err)
	}
	result := a.processImages(ctx, images, runDir, options)
	if err := cache.RemoveIfEmpty(runDir); err != nil {
		a.Log.Warning("%v", err)
	}
	var composeErr error
	if options.Up && result.Failed == 0 {
		a.Log.Section("Compose Up")
		if err := a.Docker.ComposeUp(ctx, composeFile); err != nil {
			a.Log.Warning("%v", err)
			composeErr = err
		}
	}
	a.summary(result, a.Now().Sub(started))
	if result.Failed > 0 {
		return fmt.Errorf("dpull completed with %d failure(s)", result.Failed)
	}
	if composeErr != nil {
		return fmt.Errorf("complete compose startup: %w", composeErr)
	}
	return nil
}

func (a *Application) initialize() error {
	a.Log.Section("Init")
	if err := a.Cache.CreateRoot(); err != nil {
		return fmt.Errorf("initialize cache: %w", err)
	}
	a.Log.Field("Cache", "%s", a.Cache.Root)
	a.Log.Field("Status", "ready")
	if err := cleanup.Expired(a.Cache.Root, a.Now(), time.Hour, a.Log.Warning); err != nil {
		return fmt.Errorf("clean expired cache: %w", err)
	}
	a.Log.Field("Cleanup", "expired cache removed")
	return nil
}

func (a *Application) resolveImages(options Options) ([]string, string, error) {
	if len(options.Images) > 0 {
		return unique(options.Images), options.ComposeFile, nil
	}
	path := options.ComposeFile
	if path == "" {
		var err error
		path, err = compose.Find()
		if err != nil {
			return nil, "", err
		}
	}
	images, err := compose.ParseFile(path)
	if err != nil {
		return nil, "", err
	}
	a.Log.Section("Compose")
	a.Log.Field("File", "%s", path)
	a.Log.Field("Images", "%d", len(images))
	return images, path, nil
}

func (a *Application) processImages(ctx context.Context, images []string, runDir string, options Options) Result {
	result := Result{Total: len(images)}
	for index, image := range images {
		a.Log.Separator()
		a.Log.Line("[%d/%d] %s", index+1, len(images), image)
		if err := a.processImage(ctx, image, runDir, options); err != nil {
			result.Failed++
			a.Log.Warning("%v", err)
			a.Log.Field("Result", "failed")
			continue
		}
		result.Success++
	}
	return result
}

func (a *Application) processImage(ctx context.Context, image, runDir string, options Options) error {
	tarPath := filepath.Join(runDir, util.TarFilename(image))
	a.Log.Step("checking remote image ID")
	remoteID, err := a.Crane.ImageID(ctx, image, options.Proxy)
	if err != nil {
		return err
	}
	a.Log.Field("Remote ID", "%s", remoteID)
	localID, exists, err := a.Docker.ImageID(ctx, image)
	if err != nil {
		return err
	}
	if exists {
		a.Log.Field("Local ID", "%s", localID)
	}
	if exists && localID == remoteID {
		a.Log.Field("Status", "already up to date")
		a.Log.Field("Result", "skipped")
		return nil
	}
	a.Log.Field("Status", "download required")
	a.Log.Field("Cache", "%s", tarPath)
	a.Log.Step("pulling with crane")
	if err := a.Crane.Pull(ctx, image, tarPath, options.Proxy); err != nil {
		return err
	}
	a.Log.Step("loading into Docker")
	if err := a.Docker.Load(ctx, tarPath); err != nil {
		return err
	}
	if !options.Keep {
		a.Log.Step("removing archive")
		if err := cache.RemoveFile(tarPath); err != nil {
			return err
		}
	}
	a.Log.Field("Result", "done")
	return nil
}

func (a *Application) summary(result Result, elapsed time.Duration) {
	a.Log.Separator()
	a.Log.Section("Summary")
	a.Log.Field("Images", "%d", result.Total)
	a.Log.Field("Success", "%d", result.Success)
	a.Log.Field("Failed", "%d", result.Failed)
	a.Log.Field("Elapsed", "%.1fs", elapsed.Seconds())
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
