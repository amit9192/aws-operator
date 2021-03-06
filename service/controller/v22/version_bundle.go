package v22

import (
	"github.com/giantswarm/versionbundle"
)

func VersionBundle() versionbundle.Bundle {
	return versionbundle.Bundle{
		Changelogs: []versionbundle.Changelog{
			{
				Component:   "containerlinux",
				Description: "Fix for CVE-2019-5736.",
				Kind:        versionbundle.KindSecurity,
			},
			{
				Component:   "aws-operator",
				Description: "Add support for cluster auto-scaling.",
				Kind:        versionbundle.KindAdded,
			},
			{
				Component:   "node-operator",
				Description: "Improved node draining during updates and scaling.",
				Kind:        versionbundle.KindChanged,
			},
			{
				Component:   "aws-operator",
				Description: "Lock down default Security Group.",
				Kind:        versionbundle.KindSecurity,
			},
		},
		Components: []versionbundle.Component{
			{
				Name:    "calico",
				Version: "3.2.3",
			},
			{
				Name:    "containerlinux",
				Version: "1967.5.0",
			},
			{
				Name:    "docker",
				Version: "18.06.1",
			},
			{
				Name:    "etcd",
				Version: "3.3.9",
			},
			{
				Name:    "kubernetes",
				Version: "1.12.3",
			},
		},
		Name:    "aws-operator",
		Version: "4.6.0",
	}
}
