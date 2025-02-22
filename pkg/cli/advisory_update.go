package cli

import (
	"fmt"
	"os"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"github.com/wolfi-dev/wolfictl/pkg/advisory"
	"github.com/wolfi-dev/wolfictl/pkg/cli/components/advisory/prompt"
	advisoryconfigs "github.com/wolfi-dev/wolfictl/pkg/configs/advisory"
	buildconfigs "github.com/wolfi-dev/wolfictl/pkg/configs/build"
	rwos "github.com/wolfi-dev/wolfictl/pkg/configs/rwfs/os"
	"github.com/wolfi-dev/wolfictl/pkg/distro"
	"github.com/wolfi-dev/wolfictl/pkg/index"
	"gitlab.alpinelinux.org/alpine/go/repository"
)

func AdvisoryUpdate() *cobra.Command {
	p := &updateParams{}
	cmd := &cobra.Command{
		Use:           "update",
		Short:         "append an entry to an existing package advisory",
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			archs := p.archs
			packageRepositoryURL := p.packageRepositoryURL
			distroRepoDir := resolveDistroDir(p.distroRepoDir)
			advisoriesRepoDir := resolveAdvisoriesDir(p.advisoriesRepoDir)
			if distroRepoDir == "" || advisoriesRepoDir == "" {
				if p.doNotDetectDistro {
					return fmt.Errorf("distro repo dir and/or advisories repo dir was left unspecified")
				}

				d, err := distro.Detect()
				if err != nil {
					return fmt.Errorf("distro repo dir and/or advisories repo dir was left unspecified, and distro auto-detection failed: %w", err)
				}

				if len(archs) == 0 {
					archs = d.SupportedArchitectures
				}

				if packageRepositoryURL == "" {
					packageRepositoryURL = d.APKRepositoryURL
				}

				distroRepoDir = d.DistroRepoDir
				advisoriesRepoDir = d.AdvisoriesRepoDir
				_, _ = fmt.Fprint(os.Stderr, renderDetectedDistro(d))
			}

			advisoryFsys := rwos.DirFS(advisoriesRepoDir)
			advisoryCfgs, err := advisoryconfigs.NewIndex(advisoryFsys)
			if err != nil {
				return err
			}

			fsys := rwos.DirFS(distroRepoDir)
			buildCfgs, err := buildconfigs.NewIndex(fsys)
			if err != nil {
				return fmt.Errorf("unable to select packages: %w", err)
			}

			req, err := p.requestParams.advisoryRequest()
			if err != nil {
				return err
			}

			var apkindexes []*repository.ApkIndex
			for _, arch := range archs {
				idx, err := index.Index(arch, packageRepositoryURL)
				if err != nil {
					return fmt.Errorf("unable to load APKINDEX for %s: %w", arch, err)
				}
				apkindexes = append(apkindexes, idx)
			}

			if err := req.Validate(); err != nil {
				if p.doNotPrompt {
					return fmt.Errorf("not enough information to create advisory: %w", err)
				}

				// prompt for missing fields

				allowedPackages := func() []string {
					return lo.Map(advisoryCfgs.Select().Configurations(), func(cfg advisoryconfigs.Document, _ int) string {
						return cfg.Package.Name
					})
				}

				allowedVulnerabilities := func(packageName string) []string {
					var vulnerabilities []string
					for _, cfg := range advisoryCfgs.Select().WhereName(packageName).Configurations() {
						vulns := lo.Keys(cfg.Advisories)
						sort.Strings(vulns)
						vulnerabilities = append(vulnerabilities, vulns...)
					}
					return vulnerabilities
				}

				allowedFixedVersions := newAllowedFixedVersionsFunc(apkindexes, buildCfgs)

				m := prompt.New(prompt.Configuration{
					Request:                    req,
					AllowedPackagesFunc:        allowedPackages,
					AllowedVulnerabilitiesFunc: allowedVulnerabilities,
					AllowedFixedVersionsFunc:   allowedFixedVersions,
				})
				var returnedModel tea.Model
				program := tea.NewProgram(m)

				// try to be helpful: if we're prompting, automatically enable secfixes sync
				p.requestParams.sync = true

				if returnedModel, err = program.Run(); err != nil {
					return err
				}

				if m, ok := returnedModel.(prompt.Model); ok {
					if m.EarlyExit {
						return nil
					}

					req = m.Request
				} else {
					return fmt.Errorf("unexpected model type: %T", returnedModel)
				}
			}

			opts := advisory.UpdateOptions{
				AdvisoryCfgs: advisoryCfgs,
			}

			err = advisory.Update(req, opts)
			if err != nil {
				return err
			}

			if p.requestParams.sync {
				err := doFollowupSync(advisoryCfgs.Select().WhereName(req.Package))
				if err != nil {
					return err
				}
			}

			return nil
		},
	}

	p.addFlagsTo(cmd)
	return cmd
}

type updateParams struct {
	doNotDetectDistro bool
	doNotPrompt       bool

	requestParams                    advisoryRequestParams
	distroRepoDir, advisoriesRepoDir string
	archs                            []string
	packageRepositoryURL             string
}

func (p *updateParams) addFlagsTo(cmd *cobra.Command) {
	addNoDistroDetectionFlag(&p.doNotDetectDistro, cmd)
	addNoPromptFlag(&p.doNotPrompt, cmd)

	p.requestParams.addFlags(cmd)
	addDistroDirFlag(&p.distroRepoDir, cmd)
	addAdvisoriesDirFlag(&p.advisoriesRepoDir, cmd)
	cmd.Flags().StringSliceVar(&p.archs, "arch", []string{"x86_64", "aarch64"}, "package architectures to find published versions for")
	cmd.Flags().StringVarP(&p.packageRepositoryURL, "package-repo-url", "r", "", "URL of the APK package repository")
}
