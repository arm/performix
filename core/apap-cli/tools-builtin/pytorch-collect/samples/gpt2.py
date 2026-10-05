# SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import sys
from torch import Tensor, nn
from transformers import AutoTokenizer, AutoModelForCausalLM

args = sys.argv

model: nn.Module = AutoModelForCausalLM.from_pretrained("gpt2")
model.eval()

tokenizer = AutoTokenizer.from_pretrained("gpt2")
prompt = args[1]
inputs: dict[str, Tensor] = tokenizer(prompt, return_tensors="pt")
output: Tensor = model.generate(
    **inputs, max_new_tokens=int(args[2]), do_sample=False, pad_token_id=tokenizer.eos_token_id)

print(tokenizer.decode(output[0], skip_special_tokens=True))
