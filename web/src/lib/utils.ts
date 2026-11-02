// SPDX-License-Identifier: MIT
// Adapted from shadcn/ui (https://github.com/shadcn-ui/ui), Copyright (c) 2023
// shadcn, MIT License; see THIRD_PARTY_NOTICES.md. Changed: RCONductor tokens.
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
