/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package postgres

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

const (
	ldapPassword        = "ldapPassword"
	ldapBaseDN          = "ldapBaseDN"
	ldapBindDN          = "ldapBindDN"
	ldapSearchAttribute = "ldapSearchAttribute"
	ldapSearchFilter    = "ldapSearchFilter"
	ldapServer          = "ldapServer"
	ldapPort            = 1234
	ldapScheme          = apiv1.LDAPSchemeLDAP
	ldapPrefix          = "ldapPrefix"
	ldapSuffix          = "ldapSuffix"
)

var _ = Describe("testing the building of the ldap config string", func() {
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configurationTest",
			Namespace: "default",
		},

		Spec: apiv1.ClusterSpec{
			PostgresConfiguration: apiv1.PostgresConfiguration{
				LDAP: &apiv1.LDAPConfig{
					BindSearchAuth: &apiv1.LDAPBindSearchAuth{
						BindPassword: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "testLDAPBindPasswordSecret",
							},
							Key: "key",
						},
						BaseDN:          ldapBaseDN,
						BindDN:          ldapBindDN,
						SearchAttribute: ldapSearchAttribute,
						SearchFilter:    ldapSearchFilter,
					},
					Server: ldapServer,
					Scheme: apiv1.LDAPSchemeLDAP,
					Port:   ldapPort,
					TLS:    true,
				},
			},
		},
	}
	It("returns nil for a cluster with no ldap configuration", func() {
		clusterWithoutLDAP := cluster.DeepCopy()
		clusterWithoutLDAP.Spec.PostgresConfiguration.LDAP.BindSearchAuth = nil
		clusterWithoutLDAP.Spec.PostgresConfiguration.LDAP.BindAsAuth = nil

		str := buildLDAPConfigString(clusterWithoutLDAP, ldapPassword)
		Expect(str).To(Equal(""))
	})
	It("correctly builds a bindSearchAuth string", func() {
		str := buildLDAPConfigString(&cluster, ldapPassword)
		Expect(str).To(Equal(fmt.Sprintf("host all all 0.0.0.0/0 ldap ldapserver=%s ldapport=%d "+
			"ldapscheme=%s ldaptls=1 ldapbasedn=\"%s\" ldapbinddn=\"%s\" "+
			"ldapbindpasswd=%s ldapsearchfilter=%s ldapsearchattribute=%s", ldapServer, ldapPort, ldapScheme,
			ldapBaseDN, ldapBindDN, ldapPassword, ldapSearchFilter, ldapSearchAttribute)))
	})
	It("correctly builds a bindAsAuth string", func() {
		baaCluster := cluster.DeepCopy()
		baaCluster.Spec.PostgresConfiguration.LDAP.BindSearchAuth = nil
		baaCluster.Spec.PostgresConfiguration.LDAP.BindAsAuth = &apiv1.LDAPBindAsAuth{
			Prefix: ldapPrefix,
			Suffix: ldapSuffix,
		}
		str := buildLDAPConfigString(baaCluster, ldapPassword)
		Expect(str).To(Equal(fmt.Sprintf("host all all 0.0.0.0/0 ldap ldapserver=%s ldapport=%d ldapscheme=%s "+
			"ldaptls=1 ldapprefix=\"%s\" ldapsuffix=\"%s\"", ldapServer, ldapPort, ldapScheme, ldapPrefix, ldapSuffix)))
	})
})
