/**
 * Translation Provider with Audio Subscription Management
 * Handles switching between original and translated audio streams
 */

import React, {
  createContext,
  useContext,
  useState,
  useCallback,
  useEffect,
  useRef,
} from 'react';
import {useRoomInfo, UidType} from 'customization-api';
import {RtcContext} from '../../agora-rn-uikit';
import {useUserActionMenu} from '../../src/components/useUserActionMenu';
import {TranslationMenuItem} from './TranslationMenuItem';
import SDKEvents from '../../src/utils/SdkEvents';

// Palabra UIDs start at 3000
const PALABRA_UID_BASE = 3000;

interface TranslationStream {
  language: string;
  uid: string;
  token: string;
}

interface TranslationTaskResponse {
  channel: string;
  appid: string;
  translation_streams: TranslationStream[];
  source_language: string;
  target_languages: string[];
  translation_task?: {
    task_id: string;
    success: boolean;
  };
}

interface ActiveTranslation {
  sourceUid: string;
  taskId: string;
  targetLanguage: string;
  translationUid: string;
}

interface Language {
  code: string;
  name: string;
  flag: string;
}

interface TranslationContextType {
  activeTranslations: Map<string, ActiveTranslation>;
  startTranslation: (
    sourceUid: string,
    sourceLanguage: string,
    targetLanguage: string,
  ) => Promise<void>;
  stopTranslation: (sourceUid: string) => Promise<void>;
  isTranslating: (sourceUid: string) => boolean;
  availableLanguages: Language[];
  isPalabraUid: (uid: number | string) => boolean;
}

const TranslationContext = createContext<TranslationContextType>({
  activeTranslations: new Map(),
  startTranslation: async () => {},
  stopTranslation: async () => {},
  isTranslating: () => false,
  availableLanguages: [],
  isPalabraUid: () => false,
});

export const useTranslation = () => useContext(TranslationContext);

export const TranslationProvider: React.FC<{children: React.ReactNode}> = ({
  children,
}) => {
  const [activeTranslations, setActiveTranslations] = useState<
    Map<string, ActiveTranslation>
  >(new Map());

  // Registry of all available translations in the channel (for discovery)
  const [availableTranslations, setAvailableTranslations] = useState<
    Map<string, ActiveTranslation>
  >(new Map());

  const {
    data: {channel},
  } = useRoomInfo();

  const {RtcEngineUnsafe: rtcClient} = useContext(RtcContext);
  const {updateUserActionMenuItems} = useUserActionMenu();

  // Track which remote users we're currently subscribed to
  const subscribedUsers = useRef<Set<string>>(new Set());

  /**
   * Register the translation menu item
   */
  useEffect(() => {
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
          // Translation menu action
        },
      },
    }));

    return () => {
      updateUserActionMenuItems(prevItems => {
        const {['enable-translation']: removed, ...rest} = prevItems;
        return rest;
      });
    };
  }, [updateUserActionMenuItems]);

  /**
   * Fetch existing translation tasks when joining channel
   */
  useEffect(() => {
    const fetchTasks = async () => {
      if (!channel) return;

      // Get channel name - try different properties
      const channelName = (channel as any).channel || (channel as any).name || channel;
      if (!channelName || typeof channelName !== 'string') return;

      try {
        const backendUrl = $config.PALABRA_BACKEND_ENDPOINT;
        const response = await fetch(`${backendUrl}/v1/palabra/tasks?channel=${channelName}`);

        if (!response.ok) {
          console.error('[Palabra] Failed to fetch tasks:', response.statusText);
          return;
        }

        const data = await response.json();

        // Store available translations (for discovery, not auto-subscribe)
        if (data.tasks && Array.isArray(data.tasks)) {
          const newMap = new Map<string, ActiveTranslation>();
          data.tasks.forEach((task: any) => {
            newMap.set(task.translationUid, {
              sourceUid: task.sourceUid,
              taskId: task.taskId,
              targetLanguage: task.targetLanguage,
              translationUid: task.translationUid,
            });
          });
          setAvailableTranslations(newMap);
        }
      } catch (error) {
        console.error('[Palabra] Error fetching tasks:', error);
      }
    };

    fetchTasks();
  }, [channel]);

  const availableLanguages: Language[] = [
    {code: 'es', name: 'Spanish', flag: '🇪🇸'},
    {code: 'fr', name: 'French', flag: '🇫🇷'},
    {code: 'de', name: 'German', flag: '🇩🇪'},
    {code: 'ja', name: 'Japanese', flag: '🇯🇵'},
    {code: 'zh', name: 'Chinese', flag: '🇨🇳'},
    {code: 'pt', name: 'Portuguese', flag: '🇵🇹'},
    {code: 'it', name: 'Italian', flag: '🇮🇹'},
    {code: 'ko', name: 'Korean', flag: '🇰🇷'},
  ];

  /**
   * Check if a UID is a Palabra translation stream
   */
  const isPalabraUid = useCallback((uid: number | string): boolean => {
    const numUid = typeof uid === 'string' ? parseInt(uid, 10) : uid;
    return numUid >= PALABRA_UID_BASE && numUid < PALABRA_UID_BASE + 100;
  }, []);

  /**
   * Unsubscribe from a user's audio
   */
  const unsubscribeFromUser = useCallback(
    async (uid: string) => {
      if (!rtcClient) return;

      try {
        const remoteUsers = rtcClient.remoteUsers || [];
        const user = remoteUsers.find((u: any) => u.uid.toString() === uid);

        if (user && user.audioTrack) {
          // Stop playing the audio
          user.audioTrack.stop();
          // Actually unsubscribe from the stream
          await rtcClient.unsubscribe(user, 'audio');
        }

        subscribedUsers.current.delete(uid);
      } catch (error) {
        console.error(`[Palabra] Error unsubscribing from ${uid}:`, error);
      }
    },
    [rtcClient],
  );

  /**
   * Subscribe to a user's audio
   */
  const subscribeToUser = useCallback(
    async (uid: string) => {
      if (!rtcClient) return;

      try {
        const remoteUsers = rtcClient.remoteUsers || [];
        const user = remoteUsers.find((u: any) => u.uid.toString() === uid);

        if (user && user.hasAudio && user.audioTrack) {
          await rtcClient.subscribe(user, 'audio');
          user.audioTrack.play();
          subscribedUsers.current.add(uid);
        }
      } catch (error) {
        console.error(`[Palabra] Error subscribing to ${uid}:`, error);
      }
    },
    [rtcClient],
  );

  /**
   * Start translation for a user
   */
  const startTranslation = useCallback(
    async (
      sourceUid: string,
      sourceLanguage: string,
      targetLanguage: string,
    ) => {
      try {
        // Get channel name - try different properties
        const channelName = channel.channel || channel.name || channel;

        // Call Backend
        const backendUrl = $config.PALABRA_BACKEND_ENDPOINT || $config.BACKEND_ENDPOINT;
        const url = `${backendUrl}/v1/palabra/start`;

        const response = await fetch(url, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            channel: channelName || '',
            sourceUid: sourceUid,
            sourceLanguage: sourceLanguage,
            targetLanguages: [targetLanguage],
          }),
        });

        if (!response.ok) {
          throw new Error(`Backend failed: ${response.statusText}`);
        }

        const data = await response.json();

        // Check if translation task was successful
        if (!data.success) {
          const errorMsg = data.error || 'Unknown error';
          alert(`Translation failed: ${errorMsg}`);
          throw new Error(`Palabra API error: ${errorMsg}`);
        }

        if (!data.taskId) {
          throw new Error('No task ID returned');
        }

        const translationStream = data.streams?.[0];
        if (!translationStream) {
          throw new Error('No translation stream returned');
        }

        // Unsubscribe from original audio
        await unsubscribeFromUser(sourceUid);

        // Store translation info
        const translation: ActiveTranslation = {
          sourceUid,
          taskId: data.taskId,  // Backend returns taskId directly, not data.translation_task.task_id
          targetLanguage,
          translationUid: translationStream.uid,
        };

        console.log('[Palabra] Translation task created:', {
          sourceUid,
          translationUid: translationStream.uid,
          targetLanguage,
          taskId: data.taskId,
        });

        setActiveTranslations(prev => {
          const newMap = new Map(prev);
          newMap.set(sourceUid, translation);
          console.log('[Palabra] Active translations updated:', Array.from(newMap.entries()));
          return newMap;
        });

        // The translation stream will be subscribed to via the user-published handler
        console.log('[Palabra] Waiting for translation stream UID', translationStream.uid, 'to publish...');
      } catch (error) {
        console.error('[Palabra] Failed to start translation:', error);
        // Re-subscribe to original if translation failed
        await subscribeToUser(sourceUid);
        throw error;
      }
    },
    [channel, rtcClient, unsubscribeFromUser, subscribeToUser],
  );

  /**
   * Stop translation for a user
   */
  const stopTranslation = useCallback(
    async (sourceUid: string) => {
      const translation = activeTranslations.get(sourceUid);
      if (!translation) return;

      try {
        // Call backend to stop
        const backendUrl = $config.PALABRA_BACKEND_ENDPOINT || $config.BACKEND_ENDPOINT;
        await fetch(`${backendUrl}/v1/palabra/stop`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            taskId: translation.taskId,
          }),
        });

        // Unsubscribe from translation stream
        await unsubscribeFromUser(translation.translationUid);

        // Re-subscribe to original audio
        await subscribeToUser(sourceUid);

        // Remove from active translations
        setActiveTranslations(prev => {
          const newMap = new Map(prev);
          newMap.delete(sourceUid);
          return newMap;
        });
      } catch (error) {
        console.error('[Palabra] Error stopping translation:', error);
      }
    },
    [activeTranslations, unsubscribeFromUser, subscribeToUser],
  );

  /**
   * Check if translation is active for a user
   */
  const isTranslating = useCallback(
    (sourceUid: string): boolean => {
      return activeTranslations.has(sourceUid);
    },
    [activeTranslations],
  );

  /**
   * Handle remote user published - subscribe to translation streams only if explicitly requested
   */
  useEffect(() => {
    if (!rtcClient) return;

    const handleUserPublished = async (uid: UidType, trackType: 'audio' | 'video') => {
      if (trackType !== 'audio') return;

      const uidString = uid.toString();

      // Check if this is a Palabra translation stream
      if (isPalabraUid(uid)) {
        console.log('[Palabra] Detected Palabra UID publishing:', uidString);
        console.log('[Palabra] Active translations:', Array.from(activeTranslations.entries()));

        // ONLY subscribe if this user explicitly requested this translation
        // Check activeTranslations (not availableTranslations)
        const translation = Array.from(activeTranslations.values()).find(
          t => t.translationUid === uidString,
        );

        if (translation) {
          console.log('[Palabra] Found matching translation request, subscribing to UID', uidString);
          // This user requested this translation, subscribe to it
          try {
            const remoteUsers = rtcClient.remoteUsers || [];
            const user = remoteUsers.find((u: any) => u.uid.toString() === uidString);

            if (user && user.hasAudio) {
              console.log('[Palabra] Subscribing to audio track for UID', uidString);
              await rtcClient.subscribe(user, 'audio');
              if (user.audioTrack) {
                user.audioTrack.play();
                console.log('[Palabra] Playing translated audio from UID', uidString);
              }
            } else {
              console.warn('[Palabra] User found but no audio:', {hasUser: !!user, hasAudio: user?.hasAudio});
            }
          } catch (error) {
            console.error('[Palabra] Failed to subscribe to translation:', error);
          }
        } else {
          console.log('[Palabra] Palabra UID', uidString, 'publishing but not requested by this user (ignoring)');
        }
      }
      // Regular user UIDs are handled by default App Builder logic
    };

    const unbind = SDKEvents.on('rtc-user-published', handleUserPublished);

    return () => {
      unbind();
    };
  }, [rtcClient, activeTranslations, isPalabraUid]);

  /**
   * Cleanup on unmount
   */
  useEffect(() => {
    return () => {
      activeTranslations.forEach((translation, sourceUid) => {
        stopTranslation(sourceUid);
      });
    };
  }, []);

  const value: TranslationContextType = {
    activeTranslations,
    startTranslation,
    stopTranslation,
    isTranslating,
    availableLanguages,
    isPalabraUid,
  };

  return (
    <TranslationContext.Provider value={value}>
      {children}
    </TranslationContext.Provider>
  );
};
