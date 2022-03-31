# Resource Comparison

## Motivation
When review the issues about proportion plugin and preempt action bug report, I found that the root reason of most bugs
are related with existing resource comparison functions `Less` `LessEqual` `LessEqualStrict`. After a tour of deep analysis,
I get a better understanding of scenarios and the incoordination between these scenarios and existing functions. The bugs
mainly lie at the following aspects:
* Lack of consideration about missing dimensions. When deal with the comparison of two resource lists whose dimensions
  are not all the same, default value of missing dimensions may should be considered as **zero** or **infinity**. For example,
  `L = {cpu:1c, memory:1G}`,`R = {cpu:2c, memory:2G, gpu:2}`. It's obvious that `L` has no `gpu` dimension. When default value
  of missing dimension is `zero`, it means `L = {cpu:1c, memory:1G, gpu:0}`. It's reasonable to consider `L < R` for all
  dimensions in `L` are less than that of `R`. However, when default value is `infinity`, `L` can be treated as `{cpu:1c,
  memory:1G, gpu:max}`. It's hard to say `L < R` for `gpu` in `L` is larger than that in `R`. But we still can say `L` is
  Less than `R` in some dimensions. Existing resource comparison functions all regard default value of missing dimensions
  as `zero` by default.
* Incomplete coverage of resource comparison functions. Existing resource comparison functions cannot cover all scenarios,
  which lead to some misuse for later developers. For example, when judge whether idle resource of a node can satisfy
  the request of a task, what we need is **resource amount in any dimension cannot meet the request, the node cannot meet
  the task**. So the code should be `if node.idle.LessPartly(task.request) {break}`.

In order to fix these bugs, as well as make it clear for later developers, I organize all resource comparison scenarios
and provide solution about completing existing functions as following parts.

## Recommended Functions
| Name | Comment | Example | Original function | Used plugins/actions | Transformation |
| :----:| :----: | :----: | :----: | :----: | :----: |
| l.Less(r *api.Resource, defaultValue string) | Values in all dimensions in `l` are less than that in `r` | L{cpu:1c, memory:2G} < R{cpu:2c, memory:4G} | Less(rr *Resource) | proportion | * |
| l.LessEqual(r *api.Resource, defaultValue string) | Values in all dimensions in `l` are less than or equal with that in `r` | L{cpu:1c, memory:2G} <= R{cpu:1c, memory:4G} | LessEqual(rr *Resource)/LessEqualStrict(rr *Resource) | allocate/preempt/reclaim/overcommit/proportion/reservation/topology | * |
| l.LessPartly(r *api.Resource, defaultValue string) | Values in part dimensions in `l` are less than that in `r` | L{cpu:4c, memory:2G} < \| R{cpu:2c, memory:4G} | * | topology | * |
| l.LessEqualPartly(r *api.Resource, defaultValue string) | Values in part dimensions in `l` are less than or equal with that in `r` | L{cpu:4c, memory:2G} <= \| R{cpu:2c, memory:2G} | * | * | * |
| l.Equal(r *api.Resource, defaultValue string) | Values in all dimensions in `l` are equal with that in `r` && values in all dimensions in `r` are equal with that in `l` | L{cpu:1c, memory:2G} = R{cpu:1c, memory:2G} | * | * | * |
| l.Greater(r *api.Resource, defaultValue string) | Values in all dimensions in `l` are greater than that in `r` | L{cpu:2c, memory:4G} > R{cpu:1c, memory:2G} | * | * | !l.LessEqualPartly(r *api.Resource, defaultValue string) |
| l.GreaterEqual(r *api.Resource, defaultValue string) | Values in all dimensions in `l` are greater than or equal with that in `r` | L{cpu:2c, memory:4G} >= R{cpu:2c, memory:2G} | * | * | !l.LessPartly(r *api.Resource, defaultValue string) |
| l.GreaterPartly(r *api.Resource, defaultValue string) | Values in part dimensions in `l` are greater than that in `r` | L{cpu:4c, memory:2G} > \| R{cpu:2c, memory:4G} | * | * | !l.LessEqual(r *api.Resource, defaultValue string) |
| l.GreaterEqualPartly(r *api.Resource, defaultValue string) | Values in part dimensions in `l` are greater than or equal with that in `r` | L{cpu:2c, memory:2G} >= \| R{cpu:2c, memory:4G} | * | * | !l.Less(r *api.Resource, defaultValue string) |

## Comments
* ` <| `  ` <=| `  ` >| `  ` >=| ` are self-defined mathematical symbols.
* Part scenarios are overlapped in part functions, but it makes sense when deal with specific applications.
* Part functions are not used in any plugins currently. But it's recommended to define them previously for later use.
* Parameter `defaultValue` means what value should be given to blank dimension in either of L and R. It can only be one of `zero` or `infinity`.

## Examples
* L = {cpu: 1c, memory:1G}, R = {cpu: 2c, memory:2G, gpu:2}

|  | L | R |
| :----: | :----: | :----: |
| defaultValue = zero | {cpu: 1c, memory:1G, gpu:0} | {cpu: 2c, memory:2G, gpu:2} |
| defaultValue = infinity | {cpu: 1c, memory:1G, gpu:max} | {cpu: 2c, memory:2G, gpu:2} |

|  | Less | LessEqual | LessPartly | LessEqualPartly | 
| :----: | :----: | :----: | :----: | :----: |
| defaultValue = zero | true | true | true | true |
| defaultValue = infinity | false | false | true | true |

* L = {cpu: 1c, memory:1G, gpu: 1}, R = {cpu: 2c, memory:2G}

|  | L | R |
| :----: | :----: | :----: |
| defaultValue = zero | {cpu: 1c, memory:1G, gpu:1} | {cpu: 2c, memory:2G, gpu:0} |
| defaultValue = infinity | {cpu: 1c, memory:1G, gpu:1} | {cpu: 2c, memory:2G, gpu:max} |

|  | Less | LessEqual | LessPartly | LessEqualPartly | 
| :----: | :----: | :----: | :----: | :----: |
| defaultValue = zero | false | false | true | true |
| defaultValue = infinity | true | true | true | true |

* L = {cpu: 1c, memory:1G}, R = {gpu: 2}

|  | L | R |
| :----: | :----: | :----: |
| defaultValue = zero | {cpu: 1c, memory:1G, gpu:0} | {cpu: 0c, memory:0G, gpu:2} |
| defaultValue = infinity | {cpu: 1c, memory:1G, gpu:max} | {cpu: max c, memory: max G, gpu:2} |

|  | Less | LessEqual | LessPartly | LessEqualPartly | 
| :----: | :----: | :----: | :----: | :----: |
| defaultValue = zero | false | false | true | true |
| defaultValue = infinity | false | false | true | true |