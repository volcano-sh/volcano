import numpy as np
import mindspore.context as context
from mindspore import Tensor
from mindspore.ops import functional as F
from mindspore.communication.management import init, get_rank, get_group_size

init('nccl')
context.set_context(device_target="GPU")
context.set_auto_parallel_context(parallel_mode="data_parallel", mirror_mean=True, device_num=get_group_size())

x = Tensor(np.ones([1,3,3,4]).astype(np.float32))
y = Tensor(np.ones([1,3,3,4]).astype(np.float32))
print(F.tensor_add(x, y))
