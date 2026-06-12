package servicecontrol

import (
	"filesyncengine/internal/cli"
	"filesyncengine/internal/service"
)

func Handle(opts cli.Options, configPath string) (string, error) {
	if opts.Action == cli.ActionRender {
		return service.Render(service.RenderOptions{
			Platform:   service.Platform(opts.Platform),
			BinaryPath: opts.Path,
			ConfigPath: configPath,
			User:       opts.User,
		})
	}
	return service.ControlHandoff(service.ControlOptions{
		Platform:    service.Platform(opts.Platform),
		ServiceName: opts.ID,
		Domain:      opts.Domain,
		Action:      service.ControlAction(opts.Action),
	})
}
