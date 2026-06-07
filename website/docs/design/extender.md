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
  Implement a plugin named extender, the plugin will register related functions in the SessionOpen function, and pass the snapshot to the endpoint through the http method.
  The extender plugin will choose which methods to register according to the configuration file, and is responsible for serializing and deserializing the parameters. When there is an error in the http call, the plugin decides how to deal with it according to the configuration file.
### Extender Arguments
```
- plugins:
   - name: extender
     arguments:
       extender.urlPrefix: http://127.0.0.1
       extender.httpTimeout: 100ms
       extender.onSessionOpenVerb: onSessionOpen
       extender.onSessionCloseVerb: onSessionClose
       extender.predicateVerb: predicate
       extender.prioritizeVerb: prioritize
       extender.preemptableVerb: preemptable
       extender.reclaimableVerb: reclaimable
       extender.queueOverusedVerb: queueOverused
       extender.jobEnqueueableVerb: jobEnqueueable
       extender.ignorable: true
```

### Extender Arguments Detail
  - extender.urlPrefix : Address of extender endpoint
  - extender.httpTimeout : The timeout duration for a call to the extender.
  - extender.*Verb : Verbs of extender function, ignore if verb is empty. Those verbs are appended to the urlPrefix when issuing the http call.  
  - extender.ignorable : Ignorable indicates scheduling should fail or not when this extender is unavailable.
 
### Example
```
func (ep *ExtenderPlugin) OnSessionOpen(ssn *framework.Session) {
    if len(ep.urlPrefix) == 0 {
        return
    }
    if len(ep.openSessionOpenVerb) != 0 {
        ep.onSessionOpen(ssn)
    }
    if len(ep.predicateVerb) != 0 {
        ssn.AddPredicateFn(ep.Name(), ep.predicateFn)
    }
    
    // ...
}
```
  Extender implementation should define how to handle this function call : 
  - If there are no verb exist in configuration, return nil.
  - If there are verb definition in configuration, send http request to endpoint and handle the network error.
  Plugin-based methods currently only support one Extender as an extension at the same time.
## Future Improvement
  - Support batchPredicateFn method: Add a new plugin method to calculate all nodes in one function, and implement extender method for that.
  - Support extender Bind method : Delegate bind action to extender
