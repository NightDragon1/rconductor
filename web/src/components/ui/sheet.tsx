// SPDX-License-Identifier: MIT
// Adapted from shadcn/ui (https://github.com/shadcn-ui/ui), Copyright (c) 2023
// shadcn, MIT License; see THIRD_PARTY_NOTICES.md. Changed: RCONductor tokens.
import * as SheetPrimitive from '@radix-ui/react-dialog';
import * as React from 'react';
import { cn } from '@/lib/utils';

export const Sheet = SheetPrimitive.Root;
export const SheetTrigger = SheetPrimitive.Trigger;
export const SheetClose = SheetPrimitive.Close;
export const SheetTitle = SheetPrimitive.Title;

export const SheetContent = React.forwardRef<
  React.ComponentRef<typeof SheetPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof SheetPrimitive.Content>
>(({ className, children, ...props }, ref) => (
  <SheetPrimitive.Portal>
    <SheetPrimitive.Overlay className="fixed inset-0 z-40 bg-black/60" />
    <SheetPrimitive.Content
      ref={ref}
      className={cn(
        'fixed inset-y-0 left-0 z-50 flex w-72 max-w-[85vw] flex-col border-r border-border bg-surface',
        className,
      )}
      {...props}
    >
      {children}
    </SheetPrimitive.Content>
  </SheetPrimitive.Portal>
));
SheetContent.displayName = 'SheetContent';
