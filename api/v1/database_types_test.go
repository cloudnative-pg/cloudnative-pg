package v1_test

import (
	"os"
	"path/filepath"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var _ = Describe("OptSpec", func() {
	var (
		scheme   *runtime.Scheme
		decoder  runtime.Decoder
		testData = filepath.Join("testdata", "database-with-fdw-options.yaml")
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(apiv1.AddToScheme(scheme)).To(Succeed())
		codecs := serializer.NewCodecFactory(scheme)
		decoder = codecs.UniversalDeserializer()
	})

	Context("when parsing from YAML", func() {
		It("should correctly parse OptSpec fields", func() {
			By("reading the test YAML file")
			yamlBytes, err := os.ReadFile(testData)
			Expect(err).NotTo(HaveOccurred(), "Failed to read test YAML file")

			By("decoding the YAML into a Database object")
			obj, _, err := decoder.Decode(yamlBytes, nil, nil)
			Expect(err).NotTo(HaveOccurred(), "Failed to decode YAML")
			Expect(obj).NotTo(BeNil(), "Decoded object is nil")

			By("converting to Database type")
			db, ok := obj.(*apiv1.Database)
			Expect(ok).To(BeTrue(), "Decoded object is not a Database")
			Expect(db.Spec.FDWs).NotTo(BeEmpty(), "Expected at least one FDW")

			By("verifying the OptSpec fields")
			var allOpts []apiv1.OptSpec
			for _, fdw := range db.Spec.FDWs {
				allOpts = append(allOpts, fdw.Options...)
			}

			expectedOpts := []apiv1.OptSpec{
				{
					Name:   "host",
					Value:  "postgres.example.com",
					Ensure: apiv1.EnsurePresent,
				},
				{
					Name:   "port",
					Value:  "5432",
					Ensure: apiv1.EnsurePresent,
				},
			}

			Expect(allOpts).To(HaveLen(len(expectedOpts)), "Unexpected number of options")
			for i, wantOpt := range expectedOpts {
				Expect(allOpts[i].Name).To(Equal(wantOpt.Name), "Option name mismatch")
				Expect(allOpts[i].Value).To(Equal(wantOpt.Value), "Option value mismatch")
				Expect(allOpts[i].Ensure).To(Equal(wantOpt.Ensure), "Option ensure field mismatch")
			}
		})

		Context("with invalid YAML", func() {
			It("should return an error", func() {
				invalidYAML := []byte(`
apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: test-db
spec:
  name: testdb
  fdws:
    - name: postgres_fdw
      options:
        - name: host
          ensure: invalid_ensure_value  # This should cause an error
`)

				_, _, err := decoder.Decode(invalidYAML, nil, nil)
				Expect(err).To(HaveOccurred(), "Expected error for invalid ensure value")
			})
		})
	})
})

var _ = BeforeSuite(func() {
	// Create testdata directory if it doesn't exist
	if err := os.MkdirAll("testdata", 0755); err != nil {
		panic(err)
	}

	// Create test YAML file
	testYAML := `apiVersion: postgresql.cnpg.io/v1
kind: Database
metadata:
  name: test-db
spec:
  name: testdb
  cluster:
    name: test-cluster
  fdws:
    - name: postgres_fdw
      handler: postgres_fdw_handler
      validator: postgres_fdw_validator
      options:
        - name: host
          value: postgres.example.com
          ensure: present
        - name: port
          value: "5432"
          ensure: present
`
	if err := os.WriteFile(filepath.Join("testdata", "database-with-fdw-options.yaml"), []byte(testYAML), 0644); err != nil {
		panic(err)
	}
})

var _ = AfterSuite(func() {
	// Clean up test files
	os.RemoveAll("testdata")
})
