package cmd

import (
	"bytes"
	"fmt"
	"k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	yaml2 "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"log"
	"os"
	"os/exec"
	"sigs.k8s.io/yaml"
)

type imageOptions struct {
	chart string
}

func (opt *imageOptions) runHelm3() error {
	var err error

	installManifest, err := opt.template()
	if err != nil {
		return fmt.Errorf("Failed to render chart: %s", err)
	}

	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(installManifest), 100)
	for {
		var rawObj runtime.RawExtension
		if err = decoder.Decode(&rawObj); err != nil {
			break
		}
		if len(rawObj.Raw) == 0 {
			fmt.Println(string(rawObj.Raw))
			break
		}
		obj, gvk, err := yaml2.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(rawObj.Raw, nil, nil)
		if err != nil {
			log.Fatal(err)
		}
		if gvk != nil && gvk.Kind == "Deployment" {
			deploy := &v1.Deployment{}
			out, _ := yaml.Marshal(obj.DeepCopyObject())
			yaml.Unmarshal(out, deploy)
			printImage(deploy)
		}
	}

	return nil
}

func printImage(deployment *v1.Deployment) {
	if deployment.Spec.Template.Spec.Containers != nil {
		containers := deployment.Spec.Template.Spec.Containers
		for _, container := range containers {
			fmt.Println(fmt.Sprintf("%s: %s", container.Name, container.Image))
		}
	}
}

func (opt *imageOptions) template() ([]byte, error) {
	flags := []string{}
	var (
		subcmd string
		filter func([]byte) []byte
	)
	subcmd = "template"
	filter = func(s []byte) []byte {
		return s
	}
	args := []string{subcmd, opt.chart}
	args = append(args, flags...)
	cmd := exec.Command(os.Getenv("HELM_BIN"), args...)
	out, err := outputWithRichError(cmd)
	return filter(out), err
}

func outputWithRichError(cmd *exec.Cmd) ([]byte, error) {
	output, err := cmd.Output()
	if exitError, ok := err.(*exec.ExitError); ok {
		return output, fmt.Errorf("%s: %s", exitError.Error(), string(exitError.Stderr))
	}
	return output, err
}
