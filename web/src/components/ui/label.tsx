// SPDX-License-Identifier: MIT
// Adapted from shadcn/ui (https://github.com/shadcn-ui/ui), Copyright (c) 2023
// shadcn, MIT License; see THIRD_PARTY_NOTICES.md. Changed: RCONductor tokens.
import * as React from 'react';
import { cn } from '@/lib/utils';

export const Label = React.forwardRef<
  HTMLLabelElement,
  React.LabelHTMLAttributes<HTMLLabelElement>
>(({ className, ...props }, ref) => (
  <label ref={ref} className={cn('text-sm font-medium text-text', className)} {...props} />
));
Label.displayName = 'Label';
