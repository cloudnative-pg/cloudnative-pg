#!/usr/bin/env bash

kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: snapshot-controller
  namespace: kube-system
spec:
  minReadySeconds: 35
  progressDeadlineSeconds: 600
  replicas: 2
  revisionHistoryLimit: 10
  selector:
    matchLabels:
      app.kubernetes.io/name: snapshot-controller
  strategy:
    rollingUpdate:
      maxSurge: 0
      maxUnavailable: 1
    type: RollingUpdate
  template:
    metadata:
      creationTimestamp: null
      labels:
        app.kubernetes.io/name: snapshot-controller
    spec:
      containers:
      - args:
        - --v=5
        - --leader-election=true
        - --enable-volume-group-snapshots=true
        image: registry.k8s.io/sig-storage/snapshot-controller:v7.0.1
        imagePullPolicy: IfNotPresent
        name: snapshot-controller
        resources: {}
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
      dnsPolicy: ClusterFirst
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      serviceAccount: snapshot-controller
      serviceAccountName: snapshot-controller
      terminationGracePeriodSeconds: 30
EOF

kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: StatefulSet
metadata:
  labels:
    app.kubernetes.io/component: plugin
    app.kubernetes.io/instance: hostpath.csi.k8s.io
    app.kubernetes.io/name: csi-hostpathplugin
    app.kubernetes.io/part-of: csi-driver-host-path
  name: csi-hostpathplugin
  namespace: default
spec:
  persistentVolumeClaimRetentionPolicy:
    whenDeleted: Retain
    whenScaled: Retain
  podManagementPolicy: OrderedReady
  replicas: 1
  revisionHistoryLimit: 10
  selector:
    matchLabels:
      app.kubernetes.io/component: plugin
      app.kubernetes.io/instance: hostpath.csi.k8s.io
      app.kubernetes.io/name: csi-hostpathplugin
      app.kubernetes.io/part-of: csi-driver-host-path
  serviceName: csi-hostpathplugin
  template:
    metadata:
      creationTimestamp: null
      labels:
        app.kubernetes.io/component: plugin
        app.kubernetes.io/instance: hostpath.csi.k8s.io
        app.kubernetes.io/name: csi-hostpathplugin
        app.kubernetes.io/part-of: csi-driver-host-path
    spec:
      containers:
      - args:
        - --drivername=hostpath.csi.k8s.io
        - --v=5
        - --endpoint=\$(CSI_ENDPOINT)
        - --nodeid=\$(KUBE_NODE_NAME)
        env:
        - name: CSI_ENDPOINT
          value: unix:///csi/csi.sock
        - name: KUBE_NODE_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: spec.nodeName
        image: registry.k8s.io/sig-storage/hostpathplugin:v1.13.0
        imagePullPolicy: IfNotPresent
        livenessProbe:
          failureThreshold: 5
          httpGet:
            path: /healthz
            port: healthz
            scheme: HTTP
          initialDelaySeconds: 10
          periodSeconds: 2
          successThreshold: 1
          timeoutSeconds: 3
        name: hostpath
        ports:
        - containerPort: 9898
          name: healthz
          protocol: TCP
        resources: {}
        securityContext:
          privileged: true
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
        volumeMounts:
        - mountPath: /csi
          name: socket-dir
        - mountPath: /var/lib/kubelet/pods
          mountPropagation: Bidirectional
          name: mountpoint-dir
        - mountPath: /var/lib/kubelet/plugins
          mountPropagation: Bidirectional
          name: plugins-dir
        - mountPath: /csi-data-dir
          name: csi-data-dir
        - mountPath: /dev
          name: dev-dir
      - args:
        - --v=5
        - --csi-address=\$(ADDRESS)
        - --leader-election
        env:
        - name: ADDRESS
          value: /csi/csi.sock
        image: registry.k8s.io/sig-storage/csi-external-health-monitor-controller:v0.11.0
        imagePullPolicy: IfNotPresent
        name: csi-external-health-monitor-controller
        resources: {}
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
        volumeMounts:
        - mountPath: /csi
          name: socket-dir
      - args:
        - --v=5
        - --csi-address=/csi/csi.sock
        - --kubelet-registration-path=/var/lib/kubelet/plugins/csi-hostpath/csi.sock
        env:
        - name: KUBE_NODE_NAME
          valueFrom:
            fieldRef:
              apiVersion: v1
              fieldPath: spec.nodeName
        image: registry.k8s.io/sig-storage/csi-node-driver-registrar:v2.10.0
        imagePullPolicy: IfNotPresent
        name: node-driver-registrar
        resources: {}
        securityContext:
          privileged: true
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
        volumeMounts:
        - mountPath: /csi
          name: socket-dir
        - mountPath: /registration
          name: registration-dir
        - mountPath: /csi-data-dir
          name: csi-data-dir
      - args:
        - --csi-address=/csi/csi.sock
        - --health-port=9898
        image: registry.k8s.io/sig-storage/livenessprobe:v2.12.0
        imagePullPolicy: IfNotPresent
        name: liveness-probe
        resources: {}
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
        volumeMounts:
        - mountPath: /csi
          name: socket-dir
      - args:
        - --v=5
        - --csi-address=/csi/csi.sock
        image: registry.k8s.io/sig-storage/csi-attacher:v4.5.0
        imagePullPolicy: IfNotPresent
        name: csi-attacher
        resources: {}
        securityContext:
          privileged: true
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
        volumeMounts:
        - mountPath: /csi
          name: socket-dir
      - args:
        - -v=5
        - --csi-address=/csi/csi.sock
        - --feature-gates=Topology=true
        image: registry.k8s.io/sig-storage/csi-provisioner:v4.0.0
        imagePullPolicy: IfNotPresent
        name: csi-provisioner
        resources: {}
        securityContext:
          privileged: true
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
        volumeMounts:
        - mountPath: /csi
          name: socket-dir
      - args:
        - -v=5
        - -csi-address=/csi/csi.sock
        image: registry.k8s.io/sig-storage/csi-resizer:v1.10.0
        imagePullPolicy: IfNotPresent
        name: csi-resizer
        resources: {}
        securityContext:
          privileged: true
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
        volumeMounts:
        - mountPath: /csi
          name: socket-dir
      - args:
        - -v=5
        - --enable-volume-group-snapshots=true
        - --csi-address=/csi/csi.sock
        image: registry.k8s.io/sig-storage/csi-snapshotter:v7.0.1
        imagePullPolicy: IfNotPresent
        name: csi-snapshotter
        resources: {}
        securityContext:
          privileged: true
        terminationMessagePath: /dev/termination-log
        terminationMessagePolicy: File
        volumeMounts:
        - mountPath: /csi
          name: socket-dir
      dnsPolicy: ClusterFirst
      restartPolicy: Always
      schedulerName: default-scheduler
      securityContext: {}
      serviceAccount: csi-hostpathplugin-sa
      serviceAccountName: csi-hostpathplugin-sa
      terminationGracePeriodSeconds: 30
      volumes:
      - hostPath:
          path: /var/lib/kubelet/plugins/csi-hostpath
          type: DirectoryOrCreate
        name: socket-dir
      - hostPath:
          path: /var/lib/kubelet/pods
          type: DirectoryOrCreate
        name: mountpoint-dir
      - hostPath:
          path: /var/lib/kubelet/plugins_registry
          type: Directory
        name: registration-dir
      - hostPath:
          path: /var/lib/kubelet/plugins
          type: Directory
        name: plugins-dir
      - hostPath:
          path: /var/lib/csi-hostpath-data/
          type: DirectoryOrCreate
        name: csi-data-dir
      - hostPath:
          path: /dev
          type: Directory
        name: dev-dir
  updateStrategy:
    rollingUpdate:
      partition: 0
    type: RollingUpdate
EOF

kubectl apply -f - <<EOF
---
apiVersion: groupsnapshot.storage.k8s.io/v1alpha1
kind: VolumeGroupSnapshotClass
metadata:
  name: csi-hostpath-groupsnapclass
driver: hostpath.csi.k8s.io
deletionPolicy: Delete
EOF