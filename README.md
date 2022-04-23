## 基本介绍

Helm插件是与Helm无缝集成的附加工具。插件提供一种扩展Helm核心特性集的方法，但不需要每个新的特性都用Go编写并加入核心工具中。

最近刚好有个idea，想通过插件实现下。想做一个拿来主义，发现网上竟然没有一个关于helm插件开发的教程，这让我很是忧伤！既然做不成拿来主义，那我就做一个“奶妈主义”，给大家奶一波。

## Helm插件原理

开始开发之前，我们先要弄懂helm插件是怎样实现这个插件机制的呢？我们通过源码分析可以了解到

1. helm通过插件目录获取插件元数据
2. 通过插件元数据获得执行插件的方式参数
3. 最终执行插件命令的方式将其加入到baseCmd对象中

https://github.com/helm/helm/blob/49819b4ef782e80b0c7f78c30bd76b51ebb56dc8/cmd/helm/load_plugins.go#L53

```go
func loadPlugins(baseCmd *cobra.Command, out io.Writer) {

  ...
  // 发现插件目录
	found, err := plugin.FindPlugins(settings.PluginsDirectory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load plugins: %s\n", err)
		return
	}

  // 加载插件
	for _, plug := range found {
		plug := plug
		md := plug.Metadata
		if md.Usage == "" {
			md.Usage = fmt.Sprintf("the %q plugin", md.Name)
		}
    
		c := &cobra.Command{
			Use:   md.Name,
			Short: md.Usage,
			Long:  md.Description,
			RunE: func(cmd *cobra.Command, args []string) error {
				u, err := processParent(cmd, args)
				if err != nil {
					return err
				}

				// Call setupEnv before PrepareCommand because
				// PrepareCommand uses os.ExpandEnv and expects the
				// setupEnv vars.
				plugin.SetupPluginEnv(settings, md.Name, plug.Dir)
				main, argv, prepCmdErr := plug.PrepareCommand(u)
				if prepCmdErr != nil {
					os.Stderr.WriteString(prepCmdErr.Error())
					return errors.Errorf("plugin %q exited with error", md.Name)
				}

				return callPluginExecutable(md.Name, main, argv, out)
			},
			// This passes all the flags to the subcommand.
			DisableFlagParsing: true,
		}

		// TODO: Make sure a command with this name does not already exist.
		baseCmd.AddCommand(c)

		// For completion, we try to load more details about the plugins so as to allow for command and
		// flag completion of the plugin itself.
		// We only do this when necessary (for the "completion" and "__complete" commands) to avoid the
		// risk of a rogue plugin affecting Helm's normal behavior.
		subCmd, _, err := baseCmd.Find(os.Args[1:])
		if (err == nil &&
			((subCmd.HasParent() && subCmd.Parent().Name() == "completion") || subCmd.Name() == cobra.ShellCompRequestCmd)) ||
			/* for the tests */ subCmd == baseCmd.Root() {
			loadCompletionForPlugin(c, plug)
		}
	}
}
```

## 开发需求

​    当我们使用helm进行部署的时候，我想知道我到底部署了哪些镜像咋弄。一般情况我们可以用helm template渲染出yaml文件后，再通过脚本去实现。这是一种方式，但要是能通过helm子命令直接展示来一定很不错。

​    简单点就是执行helm image [chart]，最终展示出[容器名]: [registry_addr]/[image_repo]/[image_name]:[tag]

## 环境准备

工具依赖

- helm
- goland

提前在本地准备好helm工具，安装goland。这里就不多赘述了，直接看官网文档吧。

helm安装：https://helm.sh/zh/docs/intro/install/

golang安装：https://go.dev/doc/install

## 插件开发设计

在动手开发之前，我们先大概可以先简单设计下这个实现逻辑。

1. 命令输入控制我们通过cobra去实现
2. 将chart中的镜像如何提取呢？
   1. 通过helm template [chart]转换成yaml文件
   2. 再将yaml文件转换成对应k8s资源对象
   3. 再获取k8s资源对象的容器名称和镜像地址
3. 最终将得到结果进行输出

​    这个过程中，由于template输出的是将所有的资源类型的yaml内容到放在一个大的内容里了，怎么去分隔这些资源内容，并将其转换成对应的k8s资源对象呢？这是一个比较麻烦的地方，我们来看下源码怎么实现的吧。

## 代码实现

这里大家可以先看下这里的核心代码，稍后我会做分析。完整的源码可以看这里：https://github.com/ly-wjj/helm-image

```go
import (
	"bytes"
	"fmt"
	"k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	yaml2 "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
	"log"
	"os"
	"os/exec"

)

type imageOptions struct {
	chart                    string
}

func (opt *imageOptions) runHelm3() error {
	var err error

  // 获取helm template后的信息
	installManifest, err := opt.template()
  ...
  // 将yaml内容进行转码
	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(installManifest), 100)
	for {
		var rawObj runtime.RawExtension
    // 逐个解码，当最终为EOF时即退出
		if err = decoder.Decode(&rawObj); err != nil {
			break
		}
		if len(rawObj.Raw) == 0 {
			fmt.Println(string(rawObj.Raw))
			break
		}
    // 将内容转换成一个原始对象
		obj, gvk, err := yaml2.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(rawObj.Raw, nil, nil)
		if err != nil {
			log.Fatal(err)
		}
    // 判断Kind类型，生成对应的k8s资源
		if gvk != nil && gvk.Kind == "Deployment" {
			deploy := &v1.Deployment{}
			out, _ := yaml.Marshal(obj.DeepCopyObject())
			yaml.Unmarshal(out, deploy)
			printImage(deploy)
		}
	}

	return nil
}

// 遍历输出镜像信息
func printImage(deployment *v1.Deployment) {
	if deployment.Spec.Template.Spec.Containers != nil {
		containers := deployment.Spec.Template.Spec.Containers
		for _, container := range containers {
			fmt.Println(fmt.Sprintf("%s: %s",container.Name,container.Image))
		}
	}
}

// helm template命令将chart内容转换成yaml格式
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
...

```

### 代码分析

里面涉及到一些资源的转换，后续大家在涉及到k8s资源相关开发的时候也用的到哟。

- yaml转换的内容，通过yamlutil "k8s.io/apimachinery/pkg/util/yaml"进行了转换。
- rawObj runtime.RawExtension 通过定义一个runtime的对象，decoder.Decode(&rawObj)将其转换成实例对象。最终到EOF时即退出循环。
- yaml2.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(rawObj.Raw, nil, nil)将其rawObj.Raw转换成Object, *schema.GroupVersionKind。这样我们就能知道该资源类型的情况了。
- 最后我们只需要通过schema.GroupVersionKind判断其资源类型，再通过sigs.k8s.io/yaml(务必使用这个依赖模块哟，否则会有坑)配合k8s.io/api/apps/v1转成对应的资源对象
- 最后遍历输出镜像信息

​    整个代码，还不够严谨，例如的输入参数的控制，资源类型的支持等。欢迎大家来补充。https://github.com/ly-wjj/helm-image

### 测试插件

代码编写完成后，本地测试一把。

```shell
helm repo add bitnami https://charts.bitnami.com/bitnami    
helm repo update
helm pull bitnami/kong --untar
export HELM_BIN=/opt/homebrew/bin/helm
go build -o helm-image
./helm-image image kong 
```

结果如下：

```text
kong: docker.io/bitnami/kong:2.8.1-debian-10-r16
kong-ingress-controller: docker.io/bitnami/kong-ingress-controller:2.3.1-debian-10-r12
```

## 配置插件

开发完成后，最终我们配置到helm插件。插件目录结构，一般为~/.local/share/helm/plugins

```console
$HELM_PLUGINS/
  |- helm-image /
      |
      |- plugin.yaml
      |- bin
```

将构建好的二进制文件放在对应目录下

```shell
mkdir -p ~/.local/share/helm/plugins/helm-image/bin
cp image_linux ~/.local/share/helm/plugins/helm-image/bin/helmimage
```

在~/.local/share/helm/plugins/helm-image目录下配置plugin.yaml文件

```yaml
name: "image"
version: "0.1.0"
usage: "show chart image list"
description: "show chart image list"
useTunnel: true
command: "$HELM_PLUGIN_DIR/bin/helmimage"
```

测试插件

```shell
## 执行如下命令，判断是否能够展示镜像
helm image kong
```

如果最终能够展示镜像信息，那么你的新技能helm插件开发也get了！

## 总结

本篇文章原理的内容介绍比较少，更多的是想提供一个大家后续开发helm插件，拿来即用的文档。这也算是为朋友们的工作提效尽我的绵薄之力。另外在开发这个需求的过程中，涉及到了k8s相关api的使用，也算个额外收获！

## 关注我
公众号：gungunxl
个人微信：lcomedy2021