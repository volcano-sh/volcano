# Volcano Framework Extender
## Summary
  At present, volcano has not provided a mechanism for non-invasive function realization. If the user needs to implement an internal scheduling function, the source code of volcano must be modified.
  Such as kube-scheduler, this proposal is to dynamically extend Volcano's scheduling capabilities via an http request.
## Motivation
  Currently, the only way to implement a scheduling strategy based on Volcano is to modify Volcano's code and recompile.
  This method requires developers to deeply understand the working principle of Volcano and the related architecture, which is not friendly to beginners. 
  By adding other processes and based on network communication, it will bring some performance degradation, but it will also improve scalability. Users can make their own trade-offs between the two.
### Goals
  Similar to kube-scheduler, the user-defined scheduling algorithm is registered to volcano through http server.
### Non-Goals
  Does not solve the performance degradation caused by the network.

## Design Details
### Implementation Details
  Provide Extender interface in framework package and declare related methods in Session according to the plug-in point. These methods are similar to the method signatures provided by Session, but these will return an additional error to indicate unexpected errors. When the Action calls the plugin method, the Session will call the method defined by the Extender after calling the function registered by the Plugin.
  Extender will be initialized with configuration, and session parse extender configuration into struct and save it in cache.
  when session call the methods of extender, extender method implementation will marshal arguments with json and send it to endpoint.

### Configuration
```
type ExtenderConfiguration struct {
   URLPrefix string `yaml:"urlPrefix"`
   // HTTPTimeout specifies the timeout duration for a call to the extender.
   HTTPTimeout time.Duration `yaml:"httpTimeout"`
   // Verbs of extender function, ignore if verb is empty. Those verbs are appended to the URLPrefix when issuing the http call.
   PredicateVerb      string `yaml:"predicateVerb"`
   PrioritizeVerb     string `yaml:"prioritizeVerb"`
   PreemptableVerb    string `yaml:"preemptableVerb"`
   ReclaimableVerb    string `yaml:"reclaimableVerb"`
   QueueOverusedVerb  string `yaml:"queueOverusedVerb"`
   JobEnqueueableVerb string `yaml:"jobEnqueueableVerb"`
   // Ignorable indicates scheduling should fail or not when this extender is unavailable.
   Ignorable *bool `yaml:"ignorable"`
}
```
### Extender
```
type Extender interface {
   PredicateFn(*api.TaskInfo, *api.NodeInfo) (*api.FitErrors, error)
   PrioritizeFn(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error)
 
   PreemptableFn(*api.TaskInfo, []*api.TaskInfo) ([]*api.TaskInfo, error)
   ReclaimableFn(*api.TaskInfo, []*api.TaskInfo) ([]*api.TaskInfo, error)
 
   QueueOverusedFn(api.QueueInfo) (bool, error)
   JobEnqueueableFn(*api.JobInfo) (int, error)
 
   // IsIgnorable returns true indicates scheduling should not fail when this extender is unavailable.
   IsIgnorable() bool
}
```
### Usage
```
func (ssn *Session) PredicateFn(task *api.TaskInfo, node *api.NodeInfo) error {
   for _, tier := range ssn.Tiers {
      for _, plugin := range tier.Plugins {
         if !isEnabled(plugin.EnabledPredicate) {
            continue
         }
         pfn, found := ssn.predicateFns[plugin.Name]
         if !found {
            continue
         }
         err := pfn(task, node)
         if err != nil {
            return err
         }
      }
   }
   
   for _,extender := range ssn.Extenders {
      err,fitError := extender.PredicateFn(task, node)
      if err != nil && !extender.IsIgnorable() {
         return err
      }
      if fitError != nil {
         return fitError
      }
   }
    
   return nil
}
```
  Extender implementation should define how to handle this function call : 
  - If there are no verb exist in configuration, return nil.
  - If there are verb definition in configuration, send http request to endpoint and handle the network error. 
## Future Improvement
  - Support batchPredicateFn method: Add a new plugin method to calculate all nodes in one function, and implement extender method for that.
  - Support extender OnSessionOpen/OnSessionClose method: Instead of constructing cache for Volcano Queue/Job/Task,extender will receive a snapshot when opening a new session. 
  - Support extender Bind method : Delegate bind action to extender
## FAQ
  - Why the design set extender function call at the end of session method?
    For filter-type behavior, order affects performance but not results. Based on performance considerations, the extension's function calls are finally executed.
  - Why the extender method send Volcano task/node instead of raw pod/node?
    Avoid the need to rebuild the cache when the extender needs to use the context defined by Volcano