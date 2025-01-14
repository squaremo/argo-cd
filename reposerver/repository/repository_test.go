package repository

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/argoproj/pkg/exec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/argoproj/argo-cd/pkg/apis/application/v1alpha1"
	argoappv1 "github.com/argoproj/argo-cd/pkg/apis/application/v1alpha1"
	"github.com/argoproj/argo-cd/reposerver/apiclient"
	"github.com/argoproj/argo-cd/util"
	"github.com/argoproj/argo-cd/util/cache"
	"github.com/argoproj/argo-cd/util/repo"
	"github.com/argoproj/argo-cd/util/repo/metrics"
	repomocks "github.com/argoproj/argo-cd/util/repo/mocks"
)

type fixtures struct {
	*fakeFactory
	*Service
}

func newFixtures(root, path string) *fixtures {
	factory := &fakeFactory{
		root:             root,
		path:             path,
		revision:         "aaaaaaaaaabbbbbbbbbbccccccccccdddddddddd",
		revisionMetadata: &repo.RevisionMetadata{Author: "foo", Message: strings.Repeat("x", 99), Tags: []string{"bar"}},
	}
	service := &Service{
		repoLock:    util.NewKeyLock(),
		repoFactory: factory,
		cache:       cache.NewCache(cache.NewInMemoryCache(1 * time.Hour)),
	}
	return &fixtures{factory, service}
}

type fakeFactory struct {
	root             string
	path             string
	revision         string
	revisionMetadata *repo.RevisionMetadata
}

func (f *fakeFactory) NewRepo(repo *v1alpha1.Repository, reporter metrics.Reporter) (repo.Repo, error) {
	r := repomocks.Repo{}
	root := "./testdata"
	if f.root != "" {
		root = f.root
	}
	r.On("LockKey").Return(root)
	r.On("Init").Return(nil)
	r.On("GetApp", mock.Anything, mock.Anything).Return(filepath.Join(root, f.path), nil)
	r.On("ResolveAppRevision", mock.Anything, mock.Anything).Return(f.revision, nil)
	r.On("ListApps", mock.Anything).Return(map[string]string{}, nil)
	r.On("RevisionMetadata", mock.Anything, f.revision).Return(f.revisionMetadata, nil)
	return &r, nil
}

func TestGenerateYamlManifestInDir(t *testing.T) {
	// update this value if we add/remove manifests
	const countOfManifests = 25

	q := apiclient.ManifestRequest{
		ApplicationSource: &argoappv1.ApplicationSource{},
	}
	res1, err := GenerateManifests("../../manifests/base", &q)
	assert.Nil(t, err)
	assert.Equal(t, countOfManifests, len(res1.Manifests))

	// this will test concatenated manifests to verify we split YAMLs correctly
	res2, err := GenerateManifests("./testdata/concatenated", &q)
	assert.Nil(t, err)
	assert.Equal(t, 3, len(res2.Manifests))
}

func TestService_ListApps(t *testing.T) {
	fixtures := newFixtures(".", "empty-list")
	apps, err := fixtures.Service.ListApps(context.Background(), &apiclient.ListAppsRequest{
		Repo:     &argoappv1.Repository{Repo: "my-repo"},
		Revision: "my-revision",
	})
	assert.NoError(t, err)
	assert.Equal(t, &apiclient.AppList{
		Apps: map[string]string{},
	}, apps)
}

func TestRecurseManifestsInDir(t *testing.T) {
	q := apiclient.ManifestRequest{
		ApplicationSource: &argoappv1.ApplicationSource{},
	}
	q.ApplicationSource.Directory = &argoappv1.ApplicationSourceDirectory{Recurse: true}
	res1, err := GenerateManifests("./testdata/recurse", &q)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(res1.Manifests))
}

func TestGenerateJsonnetManifestInDir(t *testing.T) {
	q := apiclient.ManifestRequest{
		ApplicationSource: &argoappv1.ApplicationSource{
			Directory: &argoappv1.ApplicationSourceDirectory{
				Jsonnet: argoappv1.ApplicationSourceJsonnet{
					ExtVars: []argoappv1.JsonnetVar{{Name: "extVarString", Value: "extVarString"}, {Name: "extVarCode", Value: "\"extVarCode\"", Code: true}},
					TLAs:    []argoappv1.JsonnetVar{{Name: "tlaString", Value: "tlaString"}, {Name: "tlaCode", Value: "\"tlaCode\"", Code: true}},
				},
			},
		},
	}
	res1, err := GenerateManifests("./testdata/jsonnet", &q)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(res1.Manifests))
}

func TestGenerateHelmChartWithDependencies(t *testing.T) {
	helmHome, err := ioutil.TempDir("", "")
	assert.NoError(t, err)
	os.Setenv("HELM_HOME", helmHome)
	_, err = exec.RunCommand("helm", exec.CmdOpts{}, "init", "--client-only", "--skip-refresh")
	assert.NoError(t, err)
	defer func() {
		_ = os.RemoveAll(helmHome)
		_ = os.RemoveAll("../../util/helm/testdata/wordpress/charts")
		os.Unsetenv("HELM_HOME")
	}()
	q := apiclient.ManifestRequest{
		ApplicationSource: &argoappv1.ApplicationSource{},
	}
	res1, err := GenerateManifests("../../util/helm/testdata/wordpress", &q)
	assert.Nil(t, err)
	assert.Len(t, res1.Manifests, 12)
}

func TestGenerateNullList(t *testing.T) {
	q := apiclient.ManifestRequest{
		ApplicationSource: &argoappv1.ApplicationSource{},
	}
	res1, err := GenerateManifests("./testdata/null-list", &q)
	assert.Nil(t, err)
	assert.Equal(t, len(res1.Manifests), 1)
	assert.Contains(t, res1.Manifests[0], "prometheus-operator-operator")

	res1, err = GenerateManifests("./testdata/empty-list", &q)
	assert.Nil(t, err)
	assert.Equal(t, len(res1.Manifests), 1)
	assert.Contains(t, res1.Manifests[0], "prometheus-operator-operator")

	res2, err := GenerateManifests("./testdata/weird-list", &q)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(res2.Manifests))
}

func TestIdentifyAppSourceTypeByAppDirWithKustomizations(t *testing.T) {
	sourceType, err := GetAppSourceType(&argoappv1.ApplicationSource{}, "./testdata/kustomization_yaml")
	assert.Nil(t, err)
	assert.Equal(t, argoappv1.ApplicationSourceTypeKustomize, sourceType)

	sourceType, err = GetAppSourceType(&argoappv1.ApplicationSource{}, "./testdata/kustomization_yml")
	assert.Nil(t, err)
	assert.Equal(t, argoappv1.ApplicationSourceTypeKustomize, sourceType)

	sourceType, err = GetAppSourceType(&argoappv1.ApplicationSource{}, "./testdata/Kustomization")
	assert.Nil(t, err)
	assert.Equal(t, argoappv1.ApplicationSourceTypeKustomize, sourceType)
}

func TestRunCustomTool(t *testing.T) {
	res, err := GenerateManifests(".", &apiclient.ManifestRequest{
		AppLabelValue: "test-app",
		Namespace:     "test-namespace",
		ApplicationSource: &argoappv1.ApplicationSource{
			Plugin: &argoappv1.ApplicationSourcePlugin{
				Name: "test",
			},
		},
		Plugins: []*argoappv1.ConfigManagementPlugin{{
			Name: "test",
			Generate: argoappv1.Command{
				Command: []string{"sh", "-c"},
				Args:    []string{`echo "{\"kind\": \"FakeObject\", \"metadata\": { \"name\": \"$ARGOCD_APP_NAME\", \"namespace\": \"$ARGOCD_APP_NAMESPACE\", \"annotations\": {\"GIT_ASKPASS\": \"$GIT_ASKPASS\", \"GIT_USERNAME\": \"$GIT_USERNAME\", \"GIT_PASSWORD\": \"$GIT_PASSWORD\"}}}"`},
			},
		}},
		Repo: &argoappv1.Repository{
			Username: "foo", Password: "bar",
		},
	})

	assert.Nil(t, err)
	assert.Equal(t, 1, len(res.Manifests))

	obj := &unstructured.Unstructured{}
	assert.Nil(t, json.Unmarshal([]byte(res.Manifests[0]), obj))

	assert.Equal(t, obj.GetName(), "test-app")
	assert.Equal(t, obj.GetNamespace(), "test-namespace")
	assert.Equal(t, "git-ask-pass.sh", obj.GetAnnotations()["GIT_ASKPASS"])
	assert.Equal(t, "foo", obj.GetAnnotations()["GIT_USERNAME"])
	assert.Equal(t, "bar", obj.GetAnnotations()["GIT_PASSWORD"])
}

func TestGenerateFromUTF16(t *testing.T) {
	q := apiclient.ManifestRequest{
		ApplicationSource: &argoappv1.ApplicationSource{},
	}
	res1, err := GenerateManifests("./testdata/utf-16", &q)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(res1.Manifests))
}

func TestGetAppDetailsHelm(t *testing.T) {
	serve := newFixtures("../../util/helm/testdata", "redis").Service
	ctx := context.Background()

	// verify default parameters are returned when not supplying values
	t.Run("DefaultParameters", func(t *testing.T) {
		res, err := serve.GetAppDetails(ctx, &apiclient.RepoServerAppDetailsQuery{
			Repo: &argoappv1.Repository{Repo: "https://github.com/fakeorg/fakerepo.git"},
			App:  "redis",
		})
		assert.NoError(t, err)
		assert.Equal(t, "Helm", res.Type)
		assert.NotNil(t, res.Helm)
		assert.Equal(t, []string{"values-production.yaml", "values.yaml"}, res.Helm.ValueFiles)
		assert.Contains(t, res.Helm.Values, "registry: docker.io")
		assert.Equal(t, argoappv1.HelmParameter{Name: "image.pullPolicy", Value: "Always"}, getHelmParameter("image.pullPolicy", res.Helm.Parameters))
		assert.Equal(t, 49, len(res.Helm.Parameters))
	})

	// verify values specific parameters are returned when a values is specified
	t.Run("SpecificParameters", func(t *testing.T) {
		res, err := serve.GetAppDetails(ctx, &apiclient.RepoServerAppDetailsQuery{
			Repo: &argoappv1.Repository{Repo: "https://github.com/fakeorg/fakerepo.git"},
			App:  "redis",
			Helm: &apiclient.HelmAppDetailsQuery{
				ValueFiles: []string{"values-production.yaml"},
			},
		})
		assert.NoError(t, err)
		assert.Equal(t, "Helm", res.Type)
		assert.NotNil(t, res.Helm)
		assert.Equal(t, []string{"values-production.yaml", "values.yaml"}, res.Helm.ValueFiles)
		assert.Contains(t, res.Helm.Values, "registry: docker.io")
		assert.Equal(t, argoappv1.HelmParameter{Name: "image.pullPolicy", Value: "IfNotPresent"}, getHelmParameter("image.pullPolicy", res.Helm.Parameters))
		assert.Equal(t, 49, len(res.Helm.Parameters))
	})
}

func getHelmParameter(name string, params []*argoappv1.HelmParameter) argoappv1.HelmParameter {
	for _, p := range params {
		if name == p.Name {
			return *p
		}
	}
	panic(name + " not in params")
}

func TestGetAppDetailsKsonnet(t *testing.T) {
	serve := newFixtures("../../test/e2e/testdata", "ksonnet").Service
	ctx := context.Background()

	res, err := serve.GetAppDetails(ctx, &apiclient.RepoServerAppDetailsQuery{
		Repo: &argoappv1.Repository{Repo: "https://github.com/fakeorg/fakerepo.git"},
		App:  "ksonnet",
	})
	assert.NoError(t, err)
	assert.Equal(t, "https://kubernetes.default.svc", res.Ksonnet.Environments["prod"].Destination.Server)
	assert.Equal(t, "prod", res.Ksonnet.Environments["prod"].Destination.Namespace)
	assert.Equal(t, "v1.10.0", res.Ksonnet.Environments["prod"].K8SVersion)
	assert.Equal(t, argoappv1.KsonnetParameter{Component: "guestbook-ui", Name: "command", Value: "null"}, *res.Ksonnet.Parameters[0])
	assert.Equal(t, 7, len(res.Ksonnet.Parameters))
}

func TestGetAppDetailsKustomize(t *testing.T) {
	serve := newFixtures("../../util/kustomize/testdata", "kustomization_yaml").Service
	ctx := context.Background()

	res, err := serve.GetAppDetails(ctx, &apiclient.RepoServerAppDetailsQuery{
		Repo: &argoappv1.Repository{Repo: "https://github.com/fakeorg/fakerepo.git"},
		App:  "kustomization_yaml",
	})
	assert.NoError(t, err)
	assert.Equal(t, []string{"nginx:1.15.4", "k8s.gcr.io/nginx-slim:0.8"}, res.Kustomize.Images)
}

func TestService_GetRevisionMetadata(t *testing.T) {
	fixtures := newFixtures(".", "empty-list")
	type args struct {
		q *apiclient.RepoServerRevisionMetadataRequest
	}
	q := &apiclient.RepoServerRevisionMetadataRequest{Repo: &argoappv1.Repository{}, App: "empty-list", Revision: fixtures.fakeFactory.revision}
	metadata := &v1alpha1.RevisionMetadata{
		Author:  fixtures.fakeFactory.revisionMetadata.Author,
		Message: strings.Repeat("x", 61) + "...",
		Tags:    fixtures.fakeFactory.revisionMetadata.Tags,
	}
	tests := []struct {
		name    string
		args    args
		want    *v1alpha1.RevisionMetadata
		wantErr bool
	}{
		{"CacheMiss", args{q: q}, metadata, false},
		{"CacheHit", args{q: q}, metadata, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fixtures.Service.GetRevisionMetadata(context.Background(), tt.args.q)
			if (err != nil) != tt.wantErr {
				t.Errorf("Service.GetRevisionMetadata() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Service.GetRevisionMetadata() = %v, want %v", got, tt.want)
			}
		})
	}
}
