/**
 * Main Customization File for Palabra Translation Integration
 * Following App Builder customization API patterns
 */

import {customize} from 'customization-api';
import React from 'react';
import {TranslationProvider} from './palabra/TranslationProvider';

/**
 * Wrapper component for VideoCall
 * Wraps the entire VideoCall with TranslationProvider to provide translation context
 */
const VideoCallWrapper: React.FC<{children: React.ReactNode}> = ({children}) => {
  return (
    <TranslationProvider>
      {children}
    </TranslationProvider>
  );
};

/**
 * Main customization export
 */
const customization = customize({
  /**
   * Wrap the VideoCall component with TranslationProvider
   */
  components: {
    videoCall: {
      wrapper: VideoCallWrapper,
    },
  },
});

export default customization;
