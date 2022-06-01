package applicationregion

import (
	"context"

	applicationmanager "g.hz.netease.com/horizon/pkg/application/manager"
	"g.hz.netease.com/horizon/pkg/applicationregion/manager"
	"g.hz.netease.com/horizon/pkg/applicationregion/models"
	envmanager "g.hz.netease.com/horizon/pkg/environment/manager"
	envregionmanager "g.hz.netease.com/horizon/pkg/environmentregion/manager"
	perror "g.hz.netease.com/horizon/pkg/errors"
	groupmanager "g.hz.netease.com/horizon/pkg/group/manager"
	regionmanager "g.hz.netease.com/horizon/pkg/region/manager"
)

type Controller interface {
	List(ctx context.Context, applicationID uint) (ApplicationRegion, error)
	Update(ctx context.Context, applicationID uint, regions ApplicationRegion) error
}

type controller struct {
	mgr                  manager.Manager
	regionMgr            regionmanager.Manager
	environmentMgr       envmanager.Manager
	environmentRegionMgr envregionmanager.Manager
	groupMgr             groupmanager.Manager
	applicationMgr       applicationmanager.Manager
}

var _ Controller = (*controller)(nil)

func NewController() Controller {
	return &controller{
		mgr:                  manager.Mgr,
		regionMgr:            regionmanager.Mgr,
		environmentMgr:       envmanager.Mgr,
		environmentRegionMgr: envregionmanager.Mgr,
		groupMgr:             groupmanager.Mgr,
		applicationMgr:       applicationmanager.Mgr,
	}
}

func (c *controller) List(ctx context.Context, applicationID uint) (ApplicationRegion, error) {
	applicationRegions, err := c.mgr.ListByApplicationID(ctx, applicationID)
	if err != nil {
		return nil, perror.WithMessage(err, "failed to list application regions")
	}

	environments, err := c.environmentMgr.ListAllEnvironment(ctx)
	if err != nil {
		return nil, perror.WithMessage(err, "failed to list environment")
	}

	application, err := c.applicationMgr.GetByID(ctx, applicationID)
	if err != nil {
		return nil, err
	}
	environmentRegions, err := c.groupMgr.GetDefaultRegions(ctx, application.GroupID)
	if err != nil {
		return nil, perror.WithMessage(err, "failed to list environmentRegions")
	}

	return ofApplicationRegion(applicationRegions, environments, environmentRegions), nil
}

func (c *controller) Update(ctx context.Context, applicationID uint, regions ApplicationRegion) error {
	applicationRegions := make([]*models.ApplicationRegion, 0)

	for _, r := range regions {
		if r.Environment != "" && r.Region != "" {
			_, err := c.environmentRegionMgr.GetByEnvironmentAndRegion(ctx, r.Environment, r.Region)
			if err != nil {
				return perror.WithMessagef(err,
					"environment/region %s/%s is not exists", r.Environment, r.Region)
			}
			applicationRegions = append(applicationRegions, &models.ApplicationRegion{
				ApplicationID:   applicationID,
				EnvironmentName: r.Environment,
				RegionName:      r.Region,
			})
		}
	}

	return c.mgr.UpsertByApplicationID(ctx, applicationID, applicationRegions)
}
