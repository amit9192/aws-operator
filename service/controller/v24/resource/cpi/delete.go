package cpi

import (
	"context"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/aws-operator/service/controller/v24/controllercontext"
	"github.com/giantswarm/aws-operator/service/controller/v24/key"
)

func (r *Resource) EnsureDeleted(ctx context.Context, obj interface{}) error {
	cr, err := key.ToCustomObject(obj)
	if err != nil {
		return microerror.Mask(err)
	}
	cc, err := controllercontext.FromContext(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	{
		r.logger.LogCtx(ctx, "level", "debug", "message", "disabling the termination protection of the tenant cluster's control plane initializer CF stack")

		i := &cloudformation.UpdateTerminationProtectionInput{
			EnableTerminationProtection: aws.Bool(false),
			StackName:                   aws.String(key.MainHostPreStackName(cr)),
		}

		_, err = cc.Client.ControlPlane.AWS.CloudFormation.UpdateTerminationProtection(i)
		if IsDeleteInProgress(err) {
			r.logger.LogCtx(ctx, "level", "debug", "message", "the tenant cluster's control plane initializer CF stack is being deleted")
			r.logger.LogCtx(ctx, "level", "debug", "message", "canceling resource")

			return nil
		} else if IsNotExists(err) {
			r.logger.LogCtx(ctx, "level", "debug", "message", "the tenant cluster's control plane initializer CF stack does not exist")
			r.logger.LogCtx(ctx, "level", "debug", "message", "canceling resource")

			return nil
		} else if err != nil {
			return microerror.Mask(err)
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", "disabled the termination protection of the tenant cluster's control plane initializer CF stack")
	}

	{
		r.logger.LogCtx(ctx, "level", "debug", "message", "requesting the deletion of the tenant cluster's control plane initializer CF stack")

		i := &cloudformation.DeleteStackInput{
			StackName: aws.String(key.MainHostPreStackName(cr)),
		}

		_, err = cc.Client.ControlPlane.AWS.CloudFormation.DeleteStack(i)
		if err != nil {
			return microerror.Mask(err)
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", "requested the deletion of the tenant cluster's control plane initializer CF stack")
	}

	return nil
}
