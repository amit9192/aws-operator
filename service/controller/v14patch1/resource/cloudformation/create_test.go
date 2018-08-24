package cloudformation

import (
	"context"
	"testing"

	awscloudformation "github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/giantswarm/apiextensions/pkg/apis/provider/v1alpha1"
	"github.com/giantswarm/micrologger/microloggertest"

	"github.com/giantswarm/aws-operator/client/aws"
	"github.com/giantswarm/aws-operator/service/controller/v14/adapter"
	"github.com/giantswarm/aws-operator/service/controller/v14/controllercontext"
)

func Test_Resource_Cloudformation_newCreate(t *testing.T) {
	t.Parallel()
	clusterTpo := &v1alpha1.AWSConfig{
		Spec: v1alpha1.AWSConfigSpec{
			Cluster: v1alpha1.Cluster{
				ID: "test-cluster",
				Kubernetes: v1alpha1.ClusterKubernetes{
					IngressController: v1alpha1.ClusterKubernetesIngressController{
						Domain: "mysubdomain.mydomain.com",
					},
					API: v1alpha1.ClusterKubernetesAPI{
						Domain: "mysubdomain.mydomain.com",
					},
				},
			},
			AWS: v1alpha1.AWSConfigSpecAWS{
				AZ: "eu-central-1a",
				Masters: []v1alpha1.AWSConfigSpecAWSNode{
					{
						ImageID: "myimageid",
					},
				},
				Region: "eu-central-1",
				Workers: []v1alpha1.AWSConfigSpecAWSNode{
					{
						ImageID: "myimageid",
					},
				},
			},
		},
	}

	testCases := []struct {
		obj               interface{}
		currentState      interface{}
		desiredState      interface{}
		expectedStackName string
		description       string
	}{
		{
			description:       "current and desired state empty, expected empty",
			obj:               clusterTpo,
			currentState:      StackState{},
			desiredState:      StackState{},
			expectedStackName: "",
		},
		{
			description:  "current state empty, desired state not empty, expected desired state",
			obj:          clusterTpo,
			currentState: StackState{},
			desiredState: StackState{
				Name: "desired",
			},
			expectedStackName: "desired",
		},
		{
			description: "current state not empty, desired state not empty but different, expected desired state",
			obj:         clusterTpo,
			currentState: StackState{
				Name: "current",
			},
			desiredState: StackState{
				Name: "desired",
			},
			expectedStackName: "desired",
		},
	}

	var err error
	var newResource *Resource
	{
		c := Config{}

		c.HostClients = &adapter.Clients{
			EC2:            &adapter.EC2ClientMock{},
			CloudFormation: &adapter.CloudFormationMock{},
			IAM:            &adapter.IAMClientMock{},
			STS:            &adapter.STSClientMock{},
		}
		c.Logger = microloggertest.New()
		c.EncrypterBackend = "kms"

		newResource, err = New(c)
		if err != nil {
			t.Fatal("expected", nil, "got", err)
		}
	}

	awsClients := aws.Clients{
		EC2: &adapter.EC2ClientMock{},
		IAM: &adapter.IAMClientMock{},
		KMS: &adapter.KMSClientMock{},
		STS: &adapter.STSClientMock{},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			ctx := context.TODO()
			ctx = controllercontext.NewContext(ctx, controllercontext.Context{AWSClient: awsClients})

			result, err := newResource.newCreateChange(ctx, tc.obj, tc.currentState, tc.desiredState)
			if err != nil {
				t.Fatal("expected", nil, "got", err)
			}
			createChange, ok := result.(awscloudformation.CreateStackInput)
			if !ok {
				t.Fatalf("expected '%T', got '%T'", createChange, result)
			}
			if createChange.StackName != nil && *createChange.StackName != tc.expectedStackName {
				t.Fatalf("expected %s, got %s", tc.expectedStackName, *createChange.StackName)
			}
		})
	}
}
