## Fields definition

### JobTemplate

`Jobtemplate` is abbreviated as `jt`, and the resource can be viewed through `kubectl get jt`

jobtemplate and vcjob can be converted to each other through vcctl

The difference between jobtemplate and vcjob is that jobtemplate will not be issued by the job controller, and jobflow can directly reference the name of the JobTemplate to implement the issuance of vcjob.

### JobFlow

`jobflow` is abbreviated as `jf`, and the resource can be viewed through `kubectl get jf`

jobflow aims to realize job-dependent operation between vcjobs in volcano. According to the dependency between vcjob, vcjob is issued.
