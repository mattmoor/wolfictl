package advisory

import (
	"fmt"

	"github.com/wolfi-dev/wolfictl/pkg/configs"
	"github.com/wolfi-dev/wolfictl/pkg/configs/advisory"
)

// CreateOptions configures the Create operation.
type CreateOptions struct {
	// AdvisoryCfgs is the Index of advisory configurations on which to operate.
	AdvisoryCfgs *configs.Index[advisory.Document]
}

// Create creates a new advisory in the `advisories` section of the configuration
// at the provided path.
func Create(req Request, opts CreateOptions) error {
	vulnID := req.Vulnerability
	advisoryEntry := req.toAdvisoryEntry()

	advisoryCfgs := opts.AdvisoryCfgs.Select().WhereName(req.Package)
	count := advisoryCfgs.Len()

	switch count {
	case 0:
		// i.e. no advisories file for this package yet
		return createAdvisoryConfig(opts.AdvisoryCfgs, req)

	case 1:
		// i.e. exactly one advisories file for this package
		u := advisory.NewAdvisoriesSectionUpdater(func(cfg advisory.Document) (advisory.Advisories, error) {
			advisories := cfg.Advisories
			if _, existsAlready := advisories[vulnID]; existsAlready {
				return advisory.Advisories{}, fmt.Errorf("advisory already exists for %s", vulnID)
			}

			advisories[vulnID] = append(advisories[vulnID], advisoryEntry)

			return advisories, nil
		})
		err := advisoryCfgs.Update(u)
		if err != nil {
			return fmt.Errorf("unable to create advisories entry in %q: %w", req.Package, err)
		}

		return nil
	}

	return fmt.Errorf("cannot create advisory: found %d advisory documents for package %q", count, req.Package)
}

func createAdvisoryConfig(cfgs *configs.Index[advisory.Document], req Request) error {
	advisories := make(advisory.Advisories)

	vulnID := req.Vulnerability
	advisories[vulnID] = append(advisories[vulnID], req.toAdvisoryEntry())

	err := cfgs.Create(fmt.Sprintf("%s.advisories.yaml", req.Package), advisory.Document{
		Package: advisory.Package{
			Name: req.Package,
		},
		Advisories: advisories,
	})
	if err != nil {
		return err
	}

	return nil
}
