package bridgezone

import (
	"context"
	"reflect"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/giantswarm/microerror"
	"github.com/giantswarm/micrologger"
	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/kubernetes"

	clientaws "github.com/giantswarm/aws-operator/client/aws"
	"github.com/giantswarm/aws-operator/service/controller/v22/controllercontext"
	"github.com/giantswarm/aws-operator/service/controller/v22/credential"
	"github.com/giantswarm/aws-operator/service/controller/v22/key"
)

const (
	name = "bridgezonev22"
)

type Config struct {
	HostAWSConfig clientaws.Config
	HostRoute53   *route53.Route53
	K8sClient     kubernetes.Interface
	Logger        micrologger.Logger

	Route53Enabled bool
}

// Resource is bridgezone resource making sure we have fallback delegation in
// old DNS structure. TODO This is only for the migration period. TODO When we
// delete the "intermediate" zone this resource becomes noop and we do not need
// it anymore.
//
// Old structure looks like:
//
//	installation.eu-central-1.aws.gigantic.io (control plane account)
//	└── NS k8s.installation.eu-central-1.aws.gigantic.io (default control plane account)
//
//	k8s.installation.eu-central-1.aws.gigantic.io (default control plane account)
//	├── A api.old_cluster_a.k8s.installation.eu-central-1.aws.gigantic.io
//	├── A ingress.old_cluster_a.k8s.installation.eu-central-1.aws.gigantic.io
//	├── A api.old_cluster_b.k8s.installation.eu-central-1.aws.gigantic.io
//	└── A ingress.old_cluster_b.k8s.installation.eu-central-1.aws.gigantic.io
//
// New structure looks like:
//
//	installation.eu-central-1.aws.gigantic.io (control plane account)
//	└── NS new_cluster_a.k8s.installation.eu-central-1.aws.gigantic.io (byoc tenant account)
//	└── NS new_cluster_b.k8s.installation.eu-central-1.aws.gigantic.io (byoc tenant account)
//
//	new_cluster_a.k8s.installation.eu-central-1.aws.gigantic.io (byoc tenant account)
//	├── A api.new_cluster_a.k8s.installation.eu-central-1.aws.gigantic.io
//	└── A ingress.new_cluster_a.k8s.installation.eu-central-1.aws.gigantic.io
//
//	new_cluster_b.k8s.installation.eu-central-1.aws.gigantic.io (byoc tenant account)
//	├── A api.new_cluster_b.k8s.installation.eu-central-1.aws.gigantic.io
//	└── A ingress.new_cluster_b.k8s.installation.eu-central-1.aws.gigantic.io
//
// For the migration period for new clusters we need also to add delegation to
// k8s.installation.eu-central-1.aws.gigantic.io because of the AWS DNS caching issues.
//
//	installation.eu-central-1.aws.gigantic.io (control plane account)
//	├── NS k8s.installation.eu-central-1.aws.gigantic.io (default tenant account)
//	└── NS cluster_id.k8s.installation.eu-central-1.aws.gigantic.io (byoc tenant account)

//	k8s.installation.eu-central-1.aws.gigantic.io (default tenant account)
//	├── NS cluster_id.k8s.installation.eu-central-1.aws.gigantic.io (byoc tenant account)
//	├── A api.old_cluster.k8s.installation.eu-central-1.aws.gigantic.io
//	└── A ingress.old_cluster.k8s.installation.eu-central-1.aws.gigantic.io

//	cluster_id.k8s.installation.eu-central-1.aws.gigantic.io (byoc tenant account)
//	├── A api.cluster_id.k8s.installation.eu-central-1.aws.gigantic.io
//	└── A ingress.cluster_id.k8s.installation.eu-central-1.aws.gigantic.io
//
// NOTE: In the code below k8s.installation.eu-central-1.aws.gigantic.io zone is called
// "intermediate" and cluster_id.k8s.installation.eu-central-1.aws.gigantic.io zone is
// called "final". This resource *only* ensures we have delegation from the
// intermediate zone to the final zone, but only if the intermediate zone
// exists.
//
// After everything is fully migrated the DNS layout should look like:
//
//	installation.eu-central-1.aws.gigantic.io (control plane account)
//	├── NS k8s.installation.eu-central-1.aws.gigantic.io (default guest account)
//	└── NS cluster_id.k8s.installation.eu-central-1.aws.gigantic.io (byoc guest account)
//
//	k8s.installation.eu-central-1.aws.gigantic.io (default guest account)
//	└── NS cluster_id.k8s.installation.eu-central-1.aws.gigantic.io (byoc guest account)
//
//	cluster_id.k8s.installation.eu-central-1.aws.gigantic.io (byoc guest account)
//	├── A api.cluster_id.k8s.installation.eu-central-1.aws.gigantic.io
//	└── A ingress.cluster_id.k8s.installation.eu-central-1.aws.gigantic.io
//
// At this point we should be fine with removing
// k8s.installation.eu-central-1.aws.gigantic.io NS record from
// installation.eu-central-1.aws.gigantic.io zone. Then after a couple of days
// when delegation propagates and DNS caches are refreshed we can delete
// k8s.installation.eu-central-1.aws.gigantic.io zone from the default guest
// account.
//
// NOTE: To complete full migration we need to start reconciling "hostpost"
// CloudFormation stack. This stack is responsible for creating
// cluster_id.k8s.installation.eu-central-1.aws.gigantic.io delegation in the
// installation.eu-central-1.aws.gigantic.io. Till this happens this resource
// cannot be deleted.
//
//	See https://github.com/giantswarm/aws-operator/pull/1373.
//
type Resource struct {
	hostAWSConfig clientaws.Config
	k8sClient     kubernetes.Interface
	logger        micrologger.Logger

	route53Enabled bool
}

func New(config Config) (*Resource, error) {
	if reflect.DeepEqual(clientaws.Config{}, config.HostAWSConfig) {
		return nil, microerror.Maskf(invalidConfigError, "%T.HostAWSConfig must not be empty", config)
	}
	if config.HostRoute53 == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.HostRoute53 must not be empty", config)
	}
	if config.K8sClient == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.K8sClient must not be empty", config)
	}
	if config.Logger == nil {
		return nil, microerror.Maskf(invalidConfigError, "%T.Logger must not be empty", config)
	}

	r := &Resource{
		hostAWSConfig: config.HostAWSConfig,
		k8sClient:     config.K8sClient,
		logger:        config.Logger,

		route53Enabled: config.Route53Enabled,
	}

	return r, nil
}

func (r *Resource) Name() string {
	return name
}

func (r *Resource) EnsureCreated(ctx context.Context, obj interface{}) error {
	customObject, err := key.ToCustomObject(obj)
	if err != nil {
		return microerror.Mask(err)
	}

	if !r.route53Enabled {
		r.logger.LogCtx(ctx, "level", "debug", "message", "route53 disabled")
		r.logger.LogCtx(ctx, "level", "debug", "message", "canceling resource reconciliation for custom object")
		return nil
	}

	baseDomain := key.BaseDomain(customObject)
	intermediateZone := "k8s." + baseDomain
	finalZone := key.ClusterID(customObject) + ".k8s." + baseDomain

	guest, defaultGuest, err := r.route53Clients(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	g := &errgroup.Group{}

	var intermediateZoneID string
	g.Go(func() error {
		r.logger.LogCtx(ctx, "level", "debug", "message", "getting intermediate zone ID")

		id, err := r.findHostedZoneID(ctx, defaultGuest, intermediateZone)
		if IsNotFound(err) {
			r.logger.LogCtx(ctx, "level", "debug", "message", "intermediate zone not found")

			return microerror.Mask(err)
		} else if err != nil {
			return microerror.Mask(err)
		}
		intermediateZoneID = id

		r.logger.LogCtx(ctx, "level", "debug", "message", "got intermediate zone ID")

		return nil
	})

	var finalZoneID string
	g.Go(func() error {
		r.logger.LogCtx(ctx, "level", "debug", "message", "getting final zone ID")

		id, err := r.findHostedZoneID(ctx, guest, finalZone)
		if IsNotFound(err) {
			r.logger.LogCtx(ctx, "level", "debug", "message", "final zone not found")

			return microerror.Mask(err)
		} else if err != nil {
			return microerror.Mask(err)
		}
		finalZoneID = id

		r.logger.LogCtx(ctx, "level", "debug", "message", "got final zone ID")

		return nil
	})

	err = g.Wait()
	if IsNotFound(err) {
		r.logger.LogCtx(ctx, "level", "debug", "message", "canceling resource")

		return nil
	} else if err != nil {
		return microerror.Mask(err)
	}

	var finalZoneRecords []*route53.ResourceRecord
	{
		r.logger.LogCtx(ctx, "level", "debug", "message", "getting final zone name servers")

		nameServers, _, err := r.getNameServersAndTTL(ctx, guest, finalZoneID, finalZone)
		if err != nil {
			return microerror.Mask(err)
		}

		for _, ns := range nameServers {
			copy := ns
			v := &route53.ResourceRecord{
				Value: &copy,
			}
			finalZoneRecords = append(finalZoneRecords, v)
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", "got final zone name servers")
	}

	{
		r.logger.LogCtx(ctx, "level", "debug", "message", "ensuring final zone delegation from intermediate zone")

		upsert := route53.ChangeActionUpsert
		ns := route53.RRTypeNs
		ttl := int64(900)

		in := &route53.ChangeResourceRecordSetsInput{
			ChangeBatch: &route53.ChangeBatch{
				Changes: []*route53.Change{
					{
						Action: &upsert,
						ResourceRecordSet: &route53.ResourceRecordSet{
							Name:            &finalZone,
							Type:            &ns,
							TTL:             &ttl,
							ResourceRecords: finalZoneRecords,
						},
					},
				},
			},
			HostedZoneId: &intermediateZoneID,
		}
		_, err := defaultGuest.ChangeResourceRecordSetsWithContext(ctx, in)
		if err != nil {
			return microerror.Mask(err)
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", "ensured final zone delegation from intermediate zone")
	}

	return nil
}

func (r *Resource) EnsureDeleted(ctx context.Context, obj interface{}) error {
	customObject, err := key.ToCustomObject(obj)
	if err != nil {
		return microerror.Mask(err)
	}

	if !r.route53Enabled {
		r.logger.LogCtx(ctx, "level", "debug", "message", "route53 disabled")
		r.logger.LogCtx(ctx, "level", "debug", "message", "canceling resource reconciliation for custom object")
		return nil
	}

	baseDomain := key.BaseDomain(customObject)
	intermediateZone := "k8s." + baseDomain
	finalZone := key.ClusterID(customObject) + ".k8s." + baseDomain

	_, defaultGuest, err := r.route53Clients(ctx)
	if err != nil {
		return microerror.Mask(err)
	}

	var intermediateZoneID string
	{
		r.logger.LogCtx(ctx, "level", "debug", "message", "getting intermediate zone ID")

		intermediateZoneID, err = r.findHostedZoneID(ctx, defaultGuest, intermediateZone)
		if IsNotFound(err) {
			r.logger.LogCtx(ctx, "level", "debug", "message", "intermediate zone not found")
			r.logger.LogCtx(ctx, "level", "debug", "message", "canceling resource reconciliation for custom object")
			return nil
		} else if err != nil {
			return microerror.Mask(err)
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", "got intermediate zone ID")
	}

	var finalZoneTTL int64
	var finalZoneRecords []*route53.ResourceRecord
	{
		r.logger.LogCtx(ctx, "level", "debug", "message", "getting final zone delegation name servers and TTL from intermediate zone")

		nameServers, ttl, err := r.getNameServersAndTTL(ctx, defaultGuest, intermediateZoneID, finalZone)
		if IsNotFound(err) {
			// Delegation may be already deleted. It must be handled.
			r.logger.LogCtx(ctx, "level", "debug", "message", "final zone delegation not found in intermediate zone")
			r.logger.LogCtx(ctx, "level", "debug", "message", "canceling resource reconciliation for custom object")
			return nil
		} else if err != nil {
			return microerror.Mask(err)
		}

		finalZoneTTL = ttl

		for _, ns := range nameServers {
			copy := ns
			v := &route53.ResourceRecord{
				Value: &copy,
			}
			finalZoneRecords = append(finalZoneRecords, v)
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", "got final zone delegation name servers and TTL from intermediate zone")
	}

	{
		r.logger.LogCtx(ctx, "level", "debug", "message", "ensuring deletion of final zone delegation from intermediate zone")

		delete := route53.ChangeActionDelete
		ns := route53.RRTypeNs

		in := &route53.ChangeResourceRecordSetsInput{
			ChangeBatch: &route53.ChangeBatch{
				Changes: []*route53.Change{
					{
						Action: &delete,
						ResourceRecordSet: &route53.ResourceRecordSet{
							Name:            &finalZone,
							Type:            &ns,
							TTL:             &finalZoneTTL,
							ResourceRecords: finalZoneRecords,
						},
					},
				},
			},
			HostedZoneId: &intermediateZoneID,
		}
		_, err := defaultGuest.ChangeResourceRecordSetsWithContext(ctx, in)
		if err != nil {
			return microerror.Mask(err)
		}

		r.logger.LogCtx(ctx, "level", "debug", "message", "ensured deletion of final zone delegation from intermediate zone")
	}

	return nil
}

// findHostedZoneID fetches Route53 hosted zone IDs based on a given name. The
// implementation fetches up to 100 matching results to find the right one. The
// bridgezone resource here is only concerned with the hosted zone ID of the
// hosted zone name provided. The desired ID will always be carried in the first
// Route53 response as the one we want to fetch is the most accurate and always
// listed as the first item in the response. This is because of the
// lexicographical order of the response items as the API documentation puts it.
// See also
// https://godoc.org/github.com/aws/aws-sdk-go/service/route53#Route53.ListHostedZonesByName.
//
//     Retrieves a list of your hosted zones in lexicographic order.
//
// Here is an example to make it clearer. Let's consider the following hosted
// zone name.
//
//     9cvgo.k8s.ginger.eu-central-1.aws.gigantic.io
//
// Given this name, findHostedZoneID will receive a response from Route53
// similar to the following example, containing a single hosted zone carrying
// its ID.
//
//     {
//       ...
//       HostedZones: [{
//         ...
//         Id: "/hostedzone/Z1A4QS1NDU6NW6",
//         Name: "9cvgo.k8s.ginger.eu-central-1.aws.gigantic.io.",
//         ...
//       }],
//       ...
//     }
//
// The example above was about a very specific domain name, which list result
// could only find a single item in the response. Let's consider a less specific
// domain name as input for findHostedZoneID.
//
//     k8s.ginger.eu-central-1.aws.gigantic.io
//
// The result from Route53 will again list all the childs within the given
// domain name. In the example response below there where only two tenant
// clusters.
//
//     {
//       ...
//       HostedZones: [{
//         ...
//         Id: "/hostedzone/Z1HJGG5VLG8GZH",
//         Name: "k8s.ginger.eu-central-1.aws.gigantic.io.",
//         ...
//       },{
//         ...
//         Id: "/hostedzone/Z1KSFLSM1JEQYM",
//         Name: "0tz6i.k8s.ginger.eu-central-1.aws.gigantic.io.",
//         ...
//       },{
//         ...
//         Id: "/hostedzone/Z1A4QS1NDU6NW6",
//         Name: "9cvgo.k8s.ginger.eu-central-1.aws.gigantic.io.",
//         ...
//       }],
//       ...
//     }
//
func (r *Resource) findHostedZoneID(ctx context.Context, client *route53.Route53, name string) (string, error) {
	in := &route53.ListHostedZonesByNameInput{
		DNSName: aws.String(name),
	}

	out, err := client.ListHostedZonesByName(in)
	if err != nil {
		return "", microerror.Mask(err)
	}

	for _, hostedZone := range out.HostedZones {
		if *hostedZone.Name == name {
			return *hostedZone.Id, nil
		}
	}

	return "", microerror.Maskf(notFoundError, "hosted zone name %#q", name)
}

func (r *Resource) getNameServersAndTTL(ctx context.Context, client *route53.Route53, zoneID, name string) (nameServers []string, ttl int64, err error) {
	one := "1"
	ns := route53.RRTypeNs
	in := &route53.ListResourceRecordSetsInput{
		HostedZoneId:    &zoneID,
		MaxItems:        &one,
		StartRecordName: &name,
		StartRecordType: &ns,
	}
	out, err := client.ListResourceRecordSetsWithContext(ctx, in)
	if err != nil {
		return nil, 0, microerror.Mask(err)
	}

	if len(out.ResourceRecordSets) == 0 {
		return nil, 0, microerror.Maskf(notFoundError, "NS record %q for HostedZone %q not found", name, zoneID)
	}
	if len(out.ResourceRecordSets) != 1 {
		return nil, 0, microerror.Maskf(executionError, "expected single NS record %q for HostedZone %q, found %#v", name, zoneID, out.ResourceRecordSets)
	}

	rs := *out.ResourceRecordSets[0]

	if strings.TrimSuffix(*rs.Name, ".") != name {
		return nil, 0, microerror.Maskf(notFoundError, "NS record %q for HostedZone %q not found", name, zoneID)
	}

	var servers []string
	for _, r := range rs.ResourceRecords {
		servers = append(servers, *r.Value)
	}

	return servers, *rs.TTL, nil
}

func (r *Resource) route53Clients(ctx context.Context) (guest, defaultGuest *route53.Route53, err error) {
	// guest
	{
		controllerCtx, err := controllercontext.FromContext(ctx)
		if err != nil {
			return nil, nil, microerror.Mask(err)
		}
		guest = controllerCtx.AWSClient.Route53
	}

	// defaultGuest
	{
		arn, err := credential.GetDefaultARN(r.k8sClient)
		if err != nil {
			return nil, nil, microerror.Mask(err)
		}

		c := r.hostAWSConfig
		c.RoleARN = arn

		newClients, err := clientaws.NewClients(c)
		if err != nil {
			return nil, nil, microerror.Mask(err)
		}

		defaultGuest = newClients.Route53
	}

	return guest, defaultGuest, nil
}
