# HPA Custom Metric Operator

Scale applications based on external metrics like queue depth or custom business metrics.

## Learning Objectives

- External metrics integration
- Custom Metrics API
- HPA integration
- Scaling decisions
- Metrics collection

## What This Operator Does

Watches for `ExternalScaler` resources and:

1. Collects metrics from external sources (RabbitMQ, Redis, etc.)
2. Exposes metrics via Custom Metrics API
3. Integrates with Horizontal Pod Autoscaler
4. Scales deployments based on custom logic
5. Reports scaling events and metrics

## Prerequisites

- Go 1.21+, Docker, kubectl, Kubernetes cluster
- RabbitMQ or Redis for testing
- Kubebuilder v3.x

## Quick Start

### 1. Deploy RabbitMQ

```bash
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rabbitmq
spec:
  selector:
    matchLabels:
      app: rabbitmq
  template:
    metadata:
      labels:
        app: rabbitmq
    spec:
      containers:
      - name: rabbitmq
        image: rabbitmq:3-management
        ports:
        - containerPort: 5672
        - containerPort: 15672
---
apiVersion: v1
kind: Service
metadata:
  name: rabbitmq
spec:
  selector:
    app: rabbitmq
  ports:
  - name: amqp
    port: 5672
  - name: management
    port: 15672
EOF
```

### 2. Create an ExternalScaler

```bash
kubectl apply -f - <<EOF
apiVersion: autoscaling.example.com/v1alpha1
kind: ExternalScaler
metadata:
  name: worker-scaler
spec:
  targetDeployment: worker
  metricSource: rabbitmq
  queueName: tasks
  targetQueueDepth: 50
  minReplicas: 1
  maxReplicas: 10
  rabbitmqURL: http://rabbitmq:15672
EOF
```

### 3. Verify Scaling

```bash
# Check scaler status
kubectl get externalscaler worker-scaler

# Publish messages to queue
kubectl run -it --rm rabbitmq-client --image=rabbitmq:3 --restart=Never -- \
  rabbitmqadmin -H rabbitmq -u guest -p guest declare queue name=tasks

# Watch deployment scale
kubectl get deployment worker -w
```

## 📖 Key Code Snippets

### CRD Definition

```go
type ExternalScalerSpec struct {
    TargetDeployment string `json:"targetDeployment"`
    MetricSource     string `json:"metricSource"` // rabbitmq, redis, http
    QueueName        string `json:"queueName"`
    TargetQueueDepth int32  `json:"targetQueueDepth"`
    MinReplicas      int32  `json:"minReplicas"`
    MaxReplicas      int32  `json:"maxReplicas"`
    RabbitmqURL      string `json:"rabbitmqURL,omitempty"`
}
```

### Metrics Collection

```go
func (r *Reconciler) getQueueDepth(ctx context.Context, scaler *ExternalScaler) (int32, error) {
    switch scaler.Spec.MetricSource {
    case "rabbitmq":
        return r.getRabbitMQQueueDepth(ctx, scaler)
    case "redis":
        return r.getRedisListLength(ctx, scaler)
    default:
        return 0, fmt.Errorf("unsupported metric source: %s", scaler.Spec.MetricSource)
    }
}

func (r *Reconciler) getRabbitMQQueueDepth(ctx context.Context, scaler *ExternalScaler) (int32, error) {
    url := fmt.Sprintf("%s/api/queues/%%2F/%s", scaler.Spec.RabbitmqURL, scaler.Spec.QueueName)
    resp, err := http.Get(url)
    if err != nil {
        return 0, err
    }
    defer resp.Body.Close()
    
    var queue struct {
        Messages int32 `json:"messages"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&queue); err != nil {
        return 0, err
    }
    
    return queue.Messages, nil
}
```

### Scaling Logic

```go
func (r *Reconciler) calculateDesiredReplicas(queueDepth, targetDepth, currentReplicas int32) int32 {
    if queueDepth == 0 {
        return 1
    }
    
    // Calculate desired replicas based on queue depth
    desired := int32(math.Ceil(float64(queueDepth) / float64(targetDepth)))
    
    // Apply min/max constraints
    if desired < r.MinReplicas {
        desired = r.MinReplicas
    }
    if desired > r.MaxReplicas {
        desired = r.MaxReplicas
    }
    
    return desired
}
```

## 🎓 Exercises

1. **Add Multiple Metrics** - Scale based on multiple metrics
2. **Implement Cooldown** - Add scaling cooldown periods
3. **Support Prometheus** - Use Prometheus as metric source
4. **Custom Scaling Algorithms** - Implement predictive scaling

## 🔗 Next Steps

- [Cluster Provisioner Operator](../../03-advanced/cluster-provisioner-operator/README.md)
- [Rolling Upgrade Operator](../../03-advanced/rolling-upgrade-operator/README.md)

---

**Excellent work on custom metrics and scaling!** 🎉
