package controllercontext

import "github.com/aws/aws-sdk-go/service/ec2"

type ContextStatus struct {
	ControlPlane  ContextStatusControlPlane
	TenantCluster ContextStatusTenantCluster
}

type ContextStatusControlPlane struct {
	AWSAccountID string
	NATGateway   ContextStatusControlPlaneNATGateway
	PeerRole     ContextStatusControlPlanePeerRole
	VPC          ContextStatusControlPlaneVPC
}

type ContextStatusControlPlaneNATGateway struct {
	Addresses []*ec2.Address
}

type ContextStatusControlPlanePeerRole struct {
	ARN string
}

type ContextStatusControlPlaneVPC struct {
	CIDR string
}

type ContextStatusTenantCluster struct {
	AWSAccountID           string
	EncryptionKey          string
	HostedZoneNameServers  string
	KMS                    ContextStatusTenantClusterKMS
	TCCP                   ContextStatusTenantClusterTCCP
	VPCPeeringConnectionID string
}

type ContextStatusTenantClusterKMS struct {
	KeyARN string
}

type ContextStatusTenantClusterTCCP struct {
	ASG ContextStatusTenantClusterTCCPASG
}
