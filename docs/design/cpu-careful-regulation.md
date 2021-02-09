# Volcano cpu careful regulation

@ProgramerGu; September 15, 2020

# Motivation
On the basis of kubernetes scheduling, the CPU fine core binding scheduling function of multi specification pod without manual intervention is realized to meet the needs of users for high-performance instances and CPU core binding, and 500 virtual machines on-line for 1 minute are guaranteed/ On this basis, the cluster resources should be balanced as much as possible to improve the utilization rate of CPU & MEM resources. Output CPU fine binding core scheduling function to the community.
# Function Detail
## data structure
node
```
apiVersion: v1
kind: Node
metadata:
  name:
  annotations:
    "cpu-map":'{"socket1": ["1", "2"],"socket2": ["11", "12"]}'
  labels:
status:
  allocatable:
    cpu: "40"
    memory: 171301360Ki
  capacity:
    cpu: "48"
    memory: 196569584Ki
    ...
```
pod
```
apiVersion: v1
kind: Pod
metadata:
  annotations:
    cpu-set: one-socket
	"cpu-map":'{"socket1": ["1"],"socket2": ["11"]}'
	//"cpu-map":'{"socket1": ["1","2"]}'
  labels:
  name:
  namespace:
spec:
    resources:
      limits:
      requests:
        cpu: "2"
```
##  scheduler-extender
- Declare to implement the scheduleextender interface
	- build node cache：build cpu map
	- managedResources: is pod annotation Declare cpu-set?
- implement the filter interface Filter
	- circular serial call: pass the filter results to the next extender,
	- remote filtering interface: return results
- priority interface(It can be left out of consideration at the initial stage)
	- parallel priority statistics
	- merge priority result
	- priority interface call: return results
- binding phase：
	- update pod to write the selected CPU device result to the pod's annotation ("CPU map": '{"Socket1": ["1", "2"]) 
	- call the bind function and write suggesthost
##  cpuset
- Plugin-pod binds the core according to the CPU device map specified in pod annotation
- Kubevirt binds the core according to the CPU device map specified in pod annotation
