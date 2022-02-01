//Package main ...
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	controller "github.com/kubermatic/image-clone-controller/pkg/controller"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	logger *zap.Logger
)

func init() {
	_ = flag.Set("logtostderr", "true")
	logger = setUpLogger()
	defer logger.Sync()
}

func setUpLogger() *zap.Logger {
	// Prepare a new logger
	atom := zap.NewAtomicLevel()
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atom,
	), zap.AddCaller()).With(zap.String("controller-name", "image-clone-controller"))

	atom.SetLevel(zap.InfoLevel)
	return logger
}

// GetClientConfig first tries to get a config object which uses the service account kubernetes gives to pods,
// if it is called from a process running in a kubernetes environment.
// Otherwise, it tries to build config from a default kubeconfig filepath if it fails, it fallback to the default config.
// Once it get the config, it returns the same.
func GetClientConfig(ctxLogger *zap.Logger) (*rest.Config, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		ctxLogger.Error("Failed to create config. Error", zap.Error(err))
		err1 := err
		kubeconfig := filepath.Join(os.Getenv("HOME"), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			err = fmt.Errorf("inClusterConfig as well as BuildConfigFromFlags Failed. Error in InClusterConfig: %+v\nError in BuildConfigFromFlags: %+v", err1, err)
			return nil, err
		}
	}

	return config, nil
}

// GetClientset first tries to get a config object which uses the service account kubernetes gives to pods,
// if it is called from a process running in a kubernetes environment.
// Otherwise, it tries to build config from a default kubeconfig filepath if it fails, it fallback to the default config.
// Once it get the config, it creates a new Clientset for the given config and returns the clientset.
func GetClientset(ctxLogger *zap.Logger) (*kubernetes.Clientset, error) {
	config, err := GetClientConfig(ctxLogger)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		err = fmt.Errorf("failed creating kubernetes clientset. Error: %+v", err)
		return nil, err
	}

	return clientset, nil
}

func main() {
	logger.Info("Starting controller for watching deployment and daemonsets")
	k8sClientset, err := GetClientset(logger)
	if err != nil {
		logger.Fatal("Failed to create kubernetes client set", zap.Error(err))
	}
	controller.RunController(k8sClientset, logger)

}
