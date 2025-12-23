/**
 * Register Translation Menu Item
 *
 * This file shows how to add the Palabra translation menu item to the
 * user action menu (3-dot menu) for remote participants.
 *
 * INTEGRATION INSTRUCTIONS:
 * 1. Import this in your app's entry point or customization-implementation
 * 2. Call registerTranslationMenuItem() after the UserActionMenuProvider is initialized
 * 3. Ensure the Lambda endpoint is configured in useTranslation.ts
 */

import React, {useEffect} from 'react';
import {useUserActionMenu} from '../../src/components/useUserActionMenu';
import {TranslationMenuItem} from './TranslationMenuItem';

/**
 * Custom hook to register the translation menu item
 * Call this in a component that's within the UserActionMenuProvider context
 */
export const useRegisterTranslationMenu = () => {
  const {updateUserActionMenuItems} = useUserActionMenu();

  useEffect(() => {
    updateUserActionMenuItems(prevItems => ({
      ...prevItems,
      'enable-translation': {
        hide: false,
        order: 10, // Position in menu (after default items)
        disabled: false,
        visibility: [
          'host-remote', // Host can translate remote users
          'attendee-remote', // Attendees can translate remote users
          'event-host-remote', // Event mode: host can translate
          'event-attendee-remote', // Event mode: attendees can translate
        ],
        component: TranslationMenuItem,
        onAction: (uid?: string | number) => {
          // Translation action
        },
      },
    }));

    return () => {
      // Cleanup if needed
    };
  }, [updateUserActionMenuItems]);
};

/**
 * React component wrapper to register the translation menu
 * Use this in your app's component tree
 */
export const TranslationMenuRegistrar: React.FC<{children?: React.ReactNode}> = ({
  children,
}) => {
  useRegisterTranslationMenu();
  return <>{children}</>;
};

/**
 * Alternative: Direct registration function (if not using hooks)
 * Call this after UserActionMenuProvider is available
 */
export const registerTranslationMenuItem = (
  updateUserActionMenuItems: (
    updater: (prev: any) => any,
  ) => void,
) => {
  updateUserActionMenuItems(prevItems => ({
    ...prevItems,
    'enable-translation': {
      hide: false,
      order: 10,
      disabled: false,
      visibility: [
        'host-remote',
        'attendee-remote',
        'event-host-remote',
        'event-attendee-remote',
      ],
      component: TranslationMenuItem,
      onAction: (uid?: string | number) => {
        // Translation action
      },
    },
  }));
};

/**
 * USAGE EXAMPLES:
 *
 * Method 1: Using the hook (recommended)
 * =====================================
 * In your App.tsx or customization-implementation/index.tsx:
 *
 * import {useRegisterTranslationMenu} from './customization/palabra/register-translation-menu';
 *
 * function YourComponent() {
 *   useRegisterTranslationMenu();
 *
 *   return (
 *     // your JSX
 *   );
 * }
 *
 *
 * Method 2: Using the wrapper component
 * ======================================
 * In your App.tsx:
 *
 * import {TranslationMenuRegistrar} from './customization/palabra/register-translation-menu';
 *
 * function App() {
 *   return (
 *     <UserActionMenuProvider>
 *       <TranslationMenuRegistrar>
 *         <YourApp />
 *       </TranslationMenuRegistrar>
 *     </UserActionMenuProvider>
 *   );
 * }
 *
 *
 * Method 3: Direct registration
 * ==============================
 * If you need more control:
 *
 * import {registerTranslationMenuItem} from './customization/palabra/register-translation-menu';
 * import {useUserActionMenu} from './src/components/useUserActionMenu';
 *
 * function setup() {
 *   const {updateUserActionMenuItems} = useUserActionMenu();
 *   registerTranslationMenuItem(updateUserActionMenuItems);
 * }
 */
