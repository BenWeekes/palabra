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
import {AvatarMenuItem, TranslationMenuItem} from './TranslationMenuItem';
import SDKEvents from '../../src/utils/SdkEvents';

// Palabra UIDs start at 3000 (audio-only translation)
const PALABRA_UID_BASE = 3000;
// Anam UIDs start at 4000 (avatar video+audio)
const ANAM_UID_BASE = 4000;

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

// Persistent Avatar session (started independently from translation)
interface AvatarSession {
  sourceUid: string;
  sessionId: string;
  anamUid: string;
  isTranslating: boolean;
  targetLanguage?: string;
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
  isAnamUid: (uid: number | string) => boolean;
  isTranslationUid: (uid: number | string) => boolean;
  // Persistent Avatar Mode
  activeAvatars: Map<string, AvatarSession>;
  startAvatar: (sourceUid: string) => Promise<void>;
  stopAvatar: (sourceUid: string) => Promise<void>;
  isAvatarActive: (sourceUid: string) => boolean;
}

const TranslationContext = createContext<TranslationContextType>({
  activeTranslations: new Map(),
  startTranslation: async () => {},
  stopTranslation: async () => {},
  isTranslating: () => false,
  availableLanguages: [],
  isPalabraUid: () => false,
  isAnamUid: () => false,
  isTranslationUid: () => false,
  // Persistent Avatar Mode
  activeAvatars: new Map(),
  startAvatar: async () => {},
  stopAvatar: async () => {},
  isAvatarActive: () => false,
});

export const useTranslation = () => useContext(TranslationContext);

export const TranslationProvider: React.FC<{children: React.ReactNode}> = ({
  children,
}) => {
  const [activeTranslations, setActiveTranslations] = useState<
    Map<string, ActiveTranslation>
  >(new Map());

  // Persistent avatar sessions (started independently from translation)
  const [activeAvatars, setActiveAvatars] = useState<Map<string, AvatarSession>>(
    new Map(),
  );

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

  // Ref to always access current activeTranslations in event handlers (avoids stale closure)
  const activeTranslationsRef = useRef<Map<string, ActiveTranslation>>(activeTranslations);

  // Ref to always access current activeAvatars in event handlers (avoids stale closure)
  const activeAvatarsRef = useRef<Map<string, AvatarSession>>(activeAvatars);

  // Ref to store original Agora subscribe function (before monkey-patch)
  const originalSubscribeRef = useRef<any>(null);

  // Keep refs in sync with state
  useEffect(() => {
    activeTranslationsRef.current = activeTranslations;
  }, [activeTranslations]);

  useEffect(() => {
    activeAvatarsRef.current = activeAvatars;
  }, [activeAvatars]);

  /**
   * OVERRIDE DEFAULT SUBSCRIPTION BEHAVIOR (no core file edits needed!)
   * Wrap the Agora SDK's subscribe() method to filter translation UIDs
   */
  useEffect(() => {
    if (!rtcClient || !(rtcClient as any).client) return;

    const client = (rtcClient as any).client;
    const originalSubscribe = client.subscribe.bind(client);

    // Store original subscribe so we can call it directly later
    originalSubscribeRef.current = originalSubscribe;

    let subscribeOverridden = false;

    // Only override once
    if (!subscribeOverridden) {
      client.subscribe = async (user: any, mediaType: 'audio' | 'video') => {
        const uidNum = typeof user.uid === 'string' ? parseInt(user.uid, 10) : user.uid;
        const uidString = user.uid.toString();
        const isTranslationUID = uidNum >= 3000 && uidNum < 5000;

        // Check if this is a translation UID (3000-4999)
        if (isTranslationUID) {
          console.log('[Palabra] 🚫 Blocking auto-subscribe for translation UID', user.uid, mediaType);
          return;
        }

        // CRITICAL: Also block sourceUid if it's currently being translated
        // This prevents dual audio (original + translation) if source re-publishes
        const isSourceBeingTranslated = activeTranslationsRef.current.has(uidString);
        if (isSourceBeingTranslated) {
          const translation = activeTranslationsRef.current.get(uidString);
          console.log('[Palabra] 🚫 Blocking auto-subscribe for sourceUid being translated:', user.uid, mediaType, '(translationUid:', translation?.translationUid, ')');
          return;
        }

        // Normal UIDs: allow subscription
        console.log('[Palabra] ✅ Allowing auto-subscribe for normal UID:', user.uid, mediaType, '(Map size:', activeTranslationsRef.current.size + ')');
        return originalSubscribe(user, mediaType);
      };
      subscribeOverridden = true;
      console.log('[Palabra] ✓ Overridden client.subscribe() to filter translation UIDs');
    }

    // No cleanup - we want this override to persist
    return () => {
      // Could restore original here if needed, but usually not necessary
    };
  }, [rtcClient]);

  /**
   * Register the avatar and translation menu items
   */
  useEffect(() => {
    updateUserActionMenuItems(prevItems => ({
      ...prevItems,
      'start-avatar': {
        hide: false,
        order: 9,
        disabled: false,
        visibility: [
          'host-remote',
          'attendee-remote',
          'event-host-remote',
          'event-attendee-remote',
        ],
        component: AvatarMenuItem,
        onAction: (uid?: string | number) => {
          // Avatar menu action
        },
      },
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
        const {['start-avatar']: removedAvatar, ['enable-translation']: removedTranslation, ...rest} = prevItems;
        return rest;
      });
    };
  }, [updateUserActionMenuItems]);

  /**
   * Fetch existing translation tasks when joining channel
   * NOTE: Disabled - /v1/palabra/tasks endpoint not implemented
   */
  // useEffect(() => {
  //   const fetchTasks = async () => {
  //     if (!channel) return;
  //
  //     const channelName = (channel as any).channel || (channel as any).name || channel;
  //     if (!channelName || typeof channelName !== 'string') return;
  //
  //     try {
  //       const backendUrl = $config.PALABRA_BACKEND_ENDPOINT;
  //       const response = await fetch(`${backendUrl}/v1/palabra/tasks?channel=${channelName}`);
  //
  //       if (!response.ok) {
  //         console.error('[Palabra] Failed to fetch tasks:', response.statusText);
  //         return;
  //       }
  //
  //       const data = await response.json();
  //
  //       if (data.tasks && Array.isArray(data.tasks)) {
  //         const newMap = new Map<string, ActiveTranslation>();
  //         data.tasks.forEach((task: any) => {
  //           newMap.set(task.translationUid, {
  //             sourceUid: task.sourceUid,
  //             taskId: task.taskId,
  //             targetLanguage: task.targetLanguage,
  //             translationUid: task.translationUid,
  //           });
  //         });
  //         setAvailableTranslations(newMap);
  //       }
  //     } catch (error) {
  //       console.error('[Palabra] Error fetching tasks:', error);
  //     }
  //   };
  //
  //   fetchTasks();
  // }, [channel]);

  const availableLanguages: Language[] = [
    {code: 'en', name: 'English', flag: '🇬🇧'},
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
   * Check if a UID is a Palabra translation stream (audio-only, 3000-3099)
   */
  const isPalabraUid = useCallback((uid: number | string): boolean => {
    const numUid = typeof uid === 'string' ? parseInt(uid, 10) : uid;
    return numUid >= PALABRA_UID_BASE && numUid < PALABRA_UID_BASE + 100;
  }, []);

  /**
   * Check if a UID is an Anam avatar stream (video+audio, 4000-4099)
   */
  const isAnamUid = useCallback((uid: number | string): boolean => {
    const numUid = typeof uid === 'string' ? parseInt(uid, 10) : uid;
    return numUid >= ANAM_UID_BASE && numUid < ANAM_UID_BASE + 100;
  }, []);

  /**
   * Check if a UID is either Palabra or Anam stream
   */
  const isTranslationUid = useCallback((uid: number | string): boolean => {
    return isPalabraUid(uid) || isAnamUid(uid);
  }, [isPalabraUid, isAnamUid]);

  /**
   * Check if avatar is active for a user (persistent avatar mode)
   */
  const isAvatarActive = useCallback(
    (sourceUid: string): boolean => {
      return activeAvatars.has(sourceUid);
    },
    [activeAvatars],
  );

  /**
   * Play video track in a 16:9 aspect ratio container
   * Creates a 16:9 box and plays video with 'cover' to fill it completely
   */
  const playVideoIn16x9Container = useCallback((videoTrack: any, containerId: string) => {
    const container = document.getElementById(containerId);
    if (!container) {
      console.log('[Palabra] ⚠️ Container not found for ID:', containerId);
      return;
    }

    // Clear existing content
    container.innerHTML = '';

    // Create wrapper that fills container and centers the 16:9 box
    const wrapper = document.createElement('div');
    wrapper.style.width = '100%';
    wrapper.style.height = '100%';
    wrapper.style.display = 'flex';
    wrapper.style.alignItems = 'center';
    wrapper.style.justifyContent = 'center';
    wrapper.style.backgroundColor = '#000';
    wrapper.style.overflow = 'hidden';

    // Create 16:9 aspect ratio container using padding trick
    const aspectContainer = document.createElement('div');
    aspectContainer.style.width = '100%';
    aspectContainer.style.position = 'relative';
    aspectContainer.style.paddingBottom = '56.25%'; // 16:9 = 9/16 = 0.5625

    // Video element positioned absolutely within the aspect container
    const videoElement = document.createElement('div');
    videoElement.style.position = 'absolute';
    videoElement.style.top = '0';
    videoElement.style.left = '0';
    videoElement.style.width = '100%';
    videoElement.style.height = '100%';

    aspectContainer.appendChild(videoElement);
    wrapper.appendChild(aspectContainer);
    container.appendChild(wrapper);

    // Play video with cover to fill the 16:9 container completely
    videoTrack.play(videoElement, {fit: 'cover'});
    console.log('[Palabra] ✓ Video playing in 16:9 container for UID', containerId);
  }, []);

  /**
   * Start persistent avatar for a user (no translation, just avatar with original audio)
   */
  const startAvatar = useCallback(
    async (sourceUid: string) => {
      try {
        const channelName = channel.channel || channel.name || channel;

        console.log('[Palabra] 🎭 Starting persistent avatar:', {
          sourceUid,
          channel: channelName,
        });

        // Call backend to start avatar
        const backendUrl = $config.PALABRA_BACKEND_ENDPOINT || $config.BACKEND_ENDPOINT;
        const url = `${backendUrl}/v1/avatar/start`;

        const response = await fetch(url, {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            channel: channelName,
            sourceUid: sourceUid,
          }),
        });

        if (!response.ok) {
          throw new Error(`Backend failed: ${response.statusText}`);
        }

        const data = await response.json();

        if (!data.success) {
          throw new Error(data.error || 'Unknown error');
        }

        console.log('[Palabra] 🎭 Avatar started:', data);

        const anamUid = data.anamUid.toString();

        // Store avatar session
        const avatarSession: AvatarSession = {
          sourceUid,
          sessionId: data.sessionId,
          anamUid: anamUid,
          isTranslating: false,
        };

        // Update ref synchronously (so event handler sees it immediately)
        const newMap = new Map(activeAvatarsRef.current);
        newMap.set(sourceUid, avatarSession);
        activeAvatarsRef.current = newMap;

        // Update state (async - triggers re-render)
        setActiveAvatars(newMap);

        console.log('[Palabra] ✓ Avatar session stored for UID', sourceUid, 'anamUid:', anamUid);

        // LATE-ARRIVAL HANDLING: Check if avatar UID already published
        const client = (rtcClient as any)?.client;
        if (client) {
          const remoteUsers = client.remoteUsers || [];
          console.log('[Palabra] 🔍 Checking for late-arrival avatar. Looking for UID:', anamUid);
          console.log('[Palabra] 🔍 remoteUsers UIDs:', remoteUsers.map((u: any) => u.uid));

          const existingUser = remoteUsers.find((u: any) => u.uid.toString() === anamUid);

          if (existingUser) {
            console.log('[Palabra] ⚡ Avatar UID', anamUid, 'already published (late arrival) - subscribing now');

            const originalSubscribe = originalSubscribeRef.current;
            if (originalSubscribe) {
              // Subscribe to video
              if (existingUser.hasVideo) {
                console.log('[Palabra] 🔄 Subscribing to avatar video for UID', anamUid);
                await originalSubscribe(existingUser, 'video');

                if (existingUser.videoTrack) {
                  // Stop original video
                  const sourceUser = remoteUsers.find((u: any) => u.uid.toString() === sourceUid);
                  if (sourceUser && sourceUser.videoTrack) {
                    sourceUser.videoTrack.stop();
                  }
                  // Play avatar video in source user's container
                  playVideoIn16x9Container(existingUser.videoTrack, sourceUid);
                  console.log('[Palabra] ✓ Avatar video playing in place of UID', sourceUid);
                }
              }

              // IMPORTANT: Unsubscribe from source user's audio FIRST
              // Otherwise user hears both original and avatar audio!
              const sourceUser = remoteUsers.find((u: any) => u.uid.toString() === sourceUid);
              if (sourceUser && sourceUser.audioTrack) {
                console.log('[Palabra] 🔇 Stopping original audio from UID', sourceUid);
                sourceUser.audioTrack.stop();
                try {
                  await client.unsubscribe(sourceUser, 'audio');
                  console.log('[Palabra] ✅ Unsubscribed from original audio for UID', sourceUid);
                } catch (e) {
                  console.log('[Palabra] ⚠️ Could not unsubscribe from original audio:', e);
                }
              }

              // Subscribe to avatar audio (avatar speaks original audio, or translated when translation active)
              if (existingUser.hasAudio) {
                console.log('[Palabra] 🔄 Subscribing to avatar audio for UID', anamUid);
                await originalSubscribe(existingUser, 'audio');
                if (existingUser.audioTrack) {
                  existingUser.audioTrack.play();
                  console.log('[Palabra] ✓ Avatar audio playing from UID', anamUid);
                }
              }
            }
          } else {
            console.log('[Palabra] ✓ Avatar session created - will subscribe when UID', anamUid, 'publishes');
          }
        }
      } catch (error) {
        console.error('[Palabra] ❌ Failed to start avatar:', error);
        throw error;
      }
    },
    [channel, rtcClient, playVideoIn16x9Container],
  );

  /**
   * Unsubscribe from a user's audio
   */
  const unsubscribeFromUser = useCallback(
    async (uid: string) => {
      if (!rtcClient) {
        console.log('[Palabra] ⚠️ Cannot unsubscribe - rtcClient not available');
        return;
      }

      const client = (rtcClient as any).client;
      if (!client) {
        console.log('[Palabra] ⚠️ Cannot unsubscribe - client not available');
        return;
      }

      try {
        // Use native SDK's remoteUsers (client), not wrapper (rtcClient)
        const remoteUsers = client.remoteUsers || [];
        const user = remoteUsers.find((u: any) => u.uid.toString() === uid);

        console.log('[Palabra] 🔇 Unsubscribing from UID', uid);
        console.log('[Palabra]   - remoteUsers count:', remoteUsers.length);
        console.log('[Palabra]   - remoteUsers UIDs:', remoteUsers.map((u: any) => u.uid));
        console.log('[Palabra]   - User found:', !!user, 'Has audio:', !!user?.audioTrack);

        if (user && user.audioTrack) {
          // Stop playing the audio
          user.audioTrack.stop();
          console.log('[Palabra] ⏹️ Stopped audio playback for UID', uid);

          // Actually unsubscribe from the stream
          await client.unsubscribe(user, 'audio');
          console.log('[Palabra] ✅ Unsubscribed from audio for UID', uid);
        } else if (user && user.hasAudio) {
          // User exists but no audioTrack - try to unsubscribe anyway
          console.log('[Palabra] ⚠️ User has audio stream but no track - trying to unsubscribe anyway');
          await client.unsubscribe(user, 'audio');
          console.log('[Palabra] ✅ Unsubscribed from audio for UID', uid);
        } else {
          console.log('[Palabra] ℹ️ UID', uid, 'not found or not publishing audio - nothing to unsubscribe');
        }

        subscribedUsers.current.delete(uid);
      } catch (error) {
        console.error(`[Palabra] ❌ Error unsubscribing from ${uid}:`, error);
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

      const client = (rtcClient as any).client;
      if (!client) return;

      try {
        const remoteUsers = client.remoteUsers || [];
        const user = remoteUsers.find((u: any) => u.uid.toString() === uid);

        console.log('[Palabra] subscribeToUser - UID:', uid, 'User found:', !!user, 'hasAudio:', user?.hasAudio);

        if (user && user.hasAudio) {
          // Use original subscribe (not monkey-patched version)
          const originalSubscribe = originalSubscribeRef.current;
          await originalSubscribe(user, 'audio');

          // After subscribe, audioTrack should be available
          if (user.audioTrack) {
            user.audioTrack.play();
            console.log('[Palabra] ✓ Subscribed and playing audio for UID', uid);
          } else {
            console.log('[Palabra] ⚠️ Subscribed but no audioTrack yet for UID', uid);
          }
          subscribedUsers.current.add(uid);
        } else {
          console.log('[Palabra] ⚠️ Cannot subscribe - user not found or no audio for UID', uid);
        }
      } catch (error) {
        console.error(`[Palabra] Error subscribing to ${uid}:`, error);
      }
    },
    [rtcClient],
  );

  /**
   * Stop persistent avatar for a user
   */
  const stopAvatar = useCallback(
    async (sourceUid: string) => {
      const avatarSession = activeAvatars.get(sourceUid);
      if (!avatarSession) {
        console.log('[Palabra] ⚠️ No avatar session found for UID', sourceUid);
        return;
      }

      try {
        const channelName = channel.channel || channel.name || channel;

        console.log('[Palabra] 🎭 Stopping avatar for UID', sourceUid);

        // Call backend to stop avatar (this also stops any active translation)
        const backendUrl = $config.PALABRA_BACKEND_ENDPOINT || $config.BACKEND_ENDPOINT;
        const url = `${backendUrl}/v1/avatar/stop`;

        await fetch(url, {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({
            channel: channelName,
            sourceUid: sourceUid,
          }),
        });

        // Unsubscribe from avatar video/audio
        const anamUid = avatarSession.anamUid;
        await unsubscribeFromUser(anamUid);

        // Remove avatar session
        setActiveAvatars(prev => {
          const newMap = new Map(prev);
          newMap.delete(sourceUid);
          return newMap;
        });

        // Also clear any active translation for this source (backend stops it too)
        if (activeTranslations.has(sourceUid)) {
          console.log('[Palabra] 🗑️ Cleaning up translation state for UID', sourceUid);
          setActiveTranslations(prev => {
            const newMap = new Map(prev);
            newMap.delete(sourceUid);
            return newMap;
          });
          activeTranslationsRef.current.delete(sourceUid);
        }

        // Re-subscribe to original video AND audio
        const client = (rtcClient as any).client;
        if (client) {
          const remoteUsers = client.remoteUsers || [];
          const sourceUser = remoteUsers.find((u: any) => u.uid.toString() === sourceUid);

          if (sourceUser) {
            const originalSubscribe = originalSubscribeRef.current;

            // Re-subscribe to audio
            if (sourceUser.hasAudio) {
              await originalSubscribe(sourceUser, 'audio');
              if (sourceUser.audioTrack) {
                sourceUser.audioTrack.play();
                console.log('[Palabra] ✓ Re-subscribed to original audio for UID', sourceUid);
              }
            }

            // Re-subscribe to video
            if (sourceUser.hasVideo) {
              await originalSubscribe(sourceUser, 'video');
              if (sourceUser.videoTrack) {
                playVideoIn16x9Container(sourceUser.videoTrack, sourceUid);
                console.log('[Palabra] ✓ Re-subscribed to original video for UID', sourceUid);
              }
            }
          }
        }

        console.log('[Palabra] ✓ Avatar stopped for UID', sourceUid);
      } catch (error) {
        console.error('[Palabra] ❌ Failed to stop avatar:', error);
        throw error;
      }
    },
    [activeAvatars, activeTranslations, channel, rtcClient, unsubscribeFromUser, playVideoIn16x9Container],
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

        console.log('[Palabra] 🚀 Starting translation:', {
          sourceUid,
          sourceLanguage,
          targetLanguage,
          channel: channelName,
        });

        // CRITICAL: Store placeholder in activeTranslations IMMEDIATELY
        // This prevents race condition where UID publishes before API response
        const placeholderTranslation = {
          sourceUid,
          translationUid: '', // Will be updated when API responds
          targetLanguage,
          taskId: '',
        };

        setActiveTranslations(prev => {
          const newMap = new Map(prev);
          newMap.set(sourceUid, placeholderTranslation);
          return newMap;
        });

        // Also update ref synchronously
        activeTranslationsRef.current.set(sourceUid, placeholderTranslation);

        console.log('[Palabra] 🔒 Pre-blocked sourceUid in Map (size now:', activeTranslationsRef.current.size, ')');

        // NOTE: Do NOT unsubscribe from original audio here
        // We wait until translation audio actually publishes to avoid audio gap

        // Call Backend
        const backendUrl = $config.PALABRA_BACKEND_ENDPOINT || $config.BACKEND_ENDPOINT;
        const url = `${backendUrl}/v1/palabra/start`;

        console.log('[Palabra] 📡 Calling backend:', url);

        const requestBody = {
          channel: channelName || '',
          sourceUid: sourceUid,
          sourceLanguage: sourceLanguage,
          targetLanguages: [targetLanguage],
        };
        console.log('[Palabra] 📤 Request body:', requestBody);

        const response = await fetch(url, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify(requestBody),
        });

        if (!response.ok) {
          throw new Error(`Backend failed: ${response.statusText}`);
        }

        const data = await response.json();

        // DEBUG: Log full backend response
        console.log('[Palabra] Backend /v1/palabra/start response:', JSON.stringify(data, null, 2));

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

        console.log('[Palabra] Translation stream UID:', translationStream.uid, 'Type:', isAnamUid(translationStream.uid) ? 'Anam (avatar)' : 'Palabra (audio-only)');

        // Check if there's a persistent avatar already running for this source
        const existingAvatar = activeAvatars.get(sourceUid);
        const hasPersistentAvatar = existingAvatar && existingAvatar.anamUid === translationStream.uid;

        // Update placeholder with real translation info
        const translation: ActiveTranslation = {
          sourceUid,
          taskId: data.taskId,
          targetLanguage,
          translationUid: translationStream.uid,
        };

        console.log('[Palabra] Translation task created:', {
          sourceUid,
          translationUid: translationStream.uid,
          targetLanguage,
          taskId: data.taskId,
          hasPersistentAvatar,
        });

        console.log('[Palabra] 🔄 Updating placeholder with real translationUid:', translationStream.uid);

        // Update ref immediately (synchronous) so late-arrival check sees it
        const newMap = new Map(activeTranslationsRef.current);
        newMap.set(sourceUid, translation);
        activeTranslationsRef.current = newMap;

        // Update state (asynchronous - triggers re-render)
        setActiveTranslations(newMap);

        console.log('[Palabra] ✓ Stored translation in activeTranslations:', {
          sourceUid,
          translationUid: translation.translationUid,
          mapSize: newMap.size,
          allEntries: Array.from(newMap.entries()).map(([k, v]) => ({
            sourceUid: k,
            translationUid: v.translationUid,
          })),
        });

        if (hasPersistentAvatar) {
          // PERSISTENT AVATAR MODE: Avatar already subscribed, backend handles audio switch
          console.log('[Palabra] 🎭 Persistent avatar active - skipping subscription, backend switches audio to translation');
          // Update avatar session state
          setActiveAvatars(prev => {
            const newMap = new Map(prev);
            const session = newMap.get(sourceUid);
            if (session) {
              newMap.set(sourceUid, { ...session, isTranslating: true, targetLanguage });
            }
            return newMap;
          });
        } else {
          // NO PERSISTENT AVATAR: Need to subscribe to translation stream
          // RACE CONDITION FIX: Check if UID already published while we were waiting for backend response
          const client = (rtcClient as any).client;
          if (client) {
            // Use native Agora SDK's remoteUsers, not App Builder's wrapper
            // App Builder's rtcClient.remoteUsers only includes subscribed users
            const remoteUsers = client.remoteUsers || [];
            console.log('[Palabra] 🔍 Checking remoteUsers for late arrival. Looking for UID:', translationStream.uid);
            console.log('[Palabra] 🔍 remoteUsers count:', remoteUsers.length);
            console.log('[Palabra] 🔍 remoteUsers UIDs:', remoteUsers.map((u: any) => u.uid));

            const existingUser = remoteUsers.find((u: any) => u.uid.toString() === translationStream.uid);
            console.log('[Palabra] 🔍 existingUser found?', !!existingUser, 'Looking for:', translationStream.uid);

            if (existingUser) {
              console.log('[Palabra] ⚡ Translation UID', translationStream.uid, 'already published (late arrival) - subscribing now');
              console.log('[Palabra] 🔍 User object before subscribe:', {
                uid: existingUser.uid,
                hasAudio: existingUser.hasAudio,
                hasVideo: existingUser.hasVideo,
                audioTrack: !!existingUser.audioTrack,
                videoTrack: !!existingUser.videoTrack,
              });

              try {
                // Use original subscribe function to bypass monkey-patch
                const originalSubscribe = originalSubscribeRef.current;
                if (!originalSubscribe) {
                  console.error('[Palabra] ❌ Original subscribe function not available');
                  return;
                }

                // Subscribe to audio for Anam UIDs or Palabra UIDs
                if ((isAnamUid(translationStream.uid) || isPalabraUid(translationStream.uid)) && existingUser.hasAudio) {
                  console.log('[Palabra] 🔄 Subscribing to audio for UID', translationStream.uid);
                  await originalSubscribe(existingUser, 'audio');

                  console.log('[Palabra] 🔍 After subscribe, user.audioTrack:', !!existingUser.audioTrack);

                  if (existingUser.audioTrack) {
                    try {
                      // NOW unsubscribe from original audio (translation audio is ready)
                      console.log('[Palabra] 🔇 Unsubscribing from original audio for UID', sourceUid);
                      await unsubscribeFromUser(sourceUid);

                      existingUser.audioTrack.play();
                      console.log('[Palabra] ✓ Playing translation audio from UID', translationStream.uid);
                    } catch (err: any) {
                      console.error('[Palabra] ❌ Failed to play audio for UID', translationStream.uid, ':', err);
                    }
                  } else {
                    console.log('[Palabra] ⚠️ No audio track on user object after subscribe for UID', translationStream.uid);
                    console.log('[Palabra] 🔍 User object keys:', Object.keys(existingUser));
                  }
                }

                // Subscribe to video for Anam UIDs (play in source user's tile)
                if (isAnamUid(translationStream.uid) && existingUser.hasVideo) {
                  console.log('[Palabra] 🔄 Subscribing to video for UID', translationStream.uid);
                  await originalSubscribe(existingUser, 'video');

                  console.log('[Palabra] 🔍 After subscribe, user.videoTrack:', !!existingUser.videoTrack);

                  if (existingUser.videoTrack) {
                    // Play Anam avatar video in the source user's tile (sourceUid from outer scope)
                    console.log('[Palabra] ✓ Playing Anam avatar video in place of source UID', sourceUid);

                    // Stop the original video if it's playing
                    const sourceUser = client.remoteUsers.find((u: any) => u.uid.toString() === sourceUid);
                    if (sourceUser && sourceUser.videoTrack) {
                      console.log('[Palabra] Stopping original video for source UID', sourceUid);
                      sourceUser.videoTrack.stop();
                    }

                    // Play Anam avatar video in the source user's container div
                    // Use 16:9 wrapper to ensure proper aspect ratio
                    playVideoIn16x9Container(existingUser.videoTrack, sourceUid);
                  } else {
                    console.log('[Palabra] ⚠️ No video track on user object after subscribe for UID', translationStream.uid);
                    console.log('[Palabra] 🔍 User object keys:', Object.keys(existingUser));
                  }
                }
              } catch (error) {
                console.error('[Palabra] ❌ Failed to subscribe to late-arrival UID', translationStream.uid, ':', error);
              }
            } else {
              console.log('[Palabra] ✓ Translation task created for UID', translationStream.uid, '- will subscribe when it publishes');
            }
          }
        }
      } catch (error) {
        console.error('[Palabra] Failed to start translation:', error);
        // Re-subscribe to original if translation failed
        await subscribeToUser(sourceUid);
        throw error;
      }
    },
    [channel, rtcClient, activeAvatars, unsubscribeFromUser, subscribeToUser, playVideoIn16x9Container],
  );

  /**
   * Stop translation for a user
   */
  const stopTranslation = useCallback(
    async (sourceUid: string) => {
      const translation = activeTranslations.get(sourceUid);
      if (!translation) return;

      // Check if there's a persistent avatar for this source
      const avatarSession = activeAvatars.get(sourceUid);
      const hasPersistentAvatar = !!avatarSession;

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

        // CRITICAL: Remove from active translations FIRST
        // Otherwise monkey-patch will block re-subscription to sourceUid
        setActiveTranslations(prev => {
          const newMap = new Map(prev);
          newMap.delete(sourceUid);
          return newMap;
        });

        // Also update ref synchronously so monkey-patch sees it immediately
        activeTranslationsRef.current.delete(sourceUid);
        console.log('[Palabra] 🔓 Removed sourceUid from Map (size now:', activeTranslationsRef.current.size, ')');

        if (hasPersistentAvatar) {
          // PERSISTENT AVATAR MODE: Don't unsubscribe from avatar, backend switches audio source
          console.log('[Palabra] 🎭 Persistent avatar active - keeping avatar video, backend switches to original audio');
          // Update avatar session state
          setActiveAvatars(prev => {
            const newMap = new Map(prev);
            const session = newMap.get(sourceUid);
            if (session) {
              newMap.set(sourceUid, { ...session, isTranslating: false, targetLanguage: undefined });
            }
            return newMap;
          });
        } else {
          // NO PERSISTENT AVATAR: Full cleanup (current behavior)
          // Unsubscribe from translation stream
          await unsubscribeFromUser(translation.translationUid);

          // Re-subscribe to original audio AND video
          const client = (rtcClient as any).client;
          if (client) {
            const remoteUsers = client.remoteUsers || [];
            const sourceUser = remoteUsers.find((u: any) => u.uid.toString() === sourceUid);

            if (sourceUser) {
              // Re-subscribe to audio
              if (sourceUser.hasAudio) {
                console.log('[Palabra] 🔊 Re-subscribing to original audio for UID', sourceUid);
                await subscribeToUser(sourceUid);
              }

              // Re-subscribe to video
              if (sourceUser.hasVideo) {
                try {
                  const originalSubscribe = originalSubscribeRef.current;
                  await originalSubscribe(sourceUser, 'video');
                  if (sourceUser.videoTrack) {
                    // Play video in the user's tile with 16:9 container
                    playVideoIn16x9Container(sourceUser.videoTrack, sourceUid);
                    console.log('[Palabra] ✓ Re-subscribed to original video for UID', sourceUid);
                  }
                } catch (error) {
                  console.error('[Palabra] Failed to re-subscribe to video:', error);
                }
              }
            }
          }
        }
      } catch (error) {
        console.error('[Palabra] Error stopping translation:', error);
      }
    },
    [activeTranslations, activeAvatars, unsubscribeFromUser, subscribeToUser, playVideoIn16x9Container],
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
   * Handle remote user published - listen directly to Agora SDK to get user object
   * Subscribe to translation streams and persistent avatar streams when explicitly requested
   */
  useEffect(() => {
    if (!rtcClient) return;

    const client = (rtcClient as any).client;
    if (!client) return;

    console.log('[Palabra] useEffect: Registering Agora user-published handler (direct SDK access)');

    const handleUserPublished = async (user: any, mediaType: 'audio' | 'video') => {
      const uidString = user.uid.toString();
      const uid = typeof user.uid === 'string' ? parseInt(user.uid, 10) : user.uid;

      // Check if this is a translation UID (3000-4999)
      if (isTranslationUid(uid)) {
        console.log('[Palabra] 📡 Translation/Avatar UID published:', uidString, 'Type:', mediaType);

        // Use refs to get current state (avoids stale closure)
        const currentTranslations = activeTranslationsRef.current;
        const currentAvatars = activeAvatarsRef.current;

        // Did I request this specific UID via translation?
        const translation = Array.from(currentTranslations.values()).find(
          t => t.translationUid === uidString,
        );

        // Did I request this specific UID via persistent avatar?
        const avatarSession = Array.from(currentAvatars.values()).find(
          a => a.anamUid === uidString,
        );

        console.log('[Palabra] Looking for UID', uidString, '- Translation:', !!translation, 'Avatar:', !!avatarSession);

        // Determine the source UID for video placement
        const sourceUid = translation?.sourceUid || avatarSession?.sourceUid;

        if (translation || avatarSession) {
          console.log('[Palabra] ✓ Requested UID', uidString, '- subscribing to', mediaType, '(source:', sourceUid, ')');

          try {
            // Use original subscribe function to bypass monkey-patch
            const originalSubscribe = originalSubscribeRef.current;
            if (!originalSubscribe) {
              console.error('[Palabra] ❌ Original subscribe function not available');
              return;
            }

            // Subscribe to audio (only for Anam UIDs 4000+)
            if (mediaType === 'audio' && isAnamUid(uid)) {
              // IMPORTANT: Always unsubscribe from source user audio first!
              // Otherwise user hears BOTH original audio and avatar audio
              if (sourceUid) {
                console.log('[Palabra] 🔇 Unsubscribing from original audio for UID', sourceUid);
                await unsubscribeFromUser(sourceUid);
              }

              await originalSubscribe(user, 'audio');
              if (user.audioTrack) {
                try {
                  user.audioTrack.play();
                  console.log('[Palabra] ✓ Playing Anam avatar audio from UID', uidString);
                } catch (err: any) {
                  console.error('[Palabra] ❌ Failed to play Anam audio:', err);
                }
              }
            } else if (mediaType === 'audio' && isPalabraUid(uid)) {
              // Subscribe to Palabra audio (3000+) when not using Anam (audio-only translation)
              await originalSubscribe(user, 'audio');
              if (user.audioTrack) {
                try {
                  if (sourceUid) {
                    console.log('[Palabra] 🔇 Unsubscribing from original audio for UID', sourceUid);
                    await unsubscribeFromUser(sourceUid);
                  }

                  user.audioTrack.play();
                  console.log('[Palabra] ✓ Playing Palabra translation audio (audio-only mode) from UID', uidString);
                } catch (err: any) {
                  console.error('[Palabra] ❌ Failed to play Palabra audio:', err);
                }
              }
            }

            // Subscribe to video (only for Anam UIDs 4000+)
            if (mediaType === 'video' && isAnamUid(uid) && sourceUid) {
              await originalSubscribe(user, 'video');
              if (user.videoTrack) {
                console.log('[Palabra] ✓ Playing Anam avatar video in place of source UID', sourceUid);

                // Stop the original video if it's playing
                const sourceUser = client.remoteUsers.find((u: any) => u.uid.toString() === sourceUid);
                if (sourceUser && sourceUser.videoTrack) {
                  console.log('[Palabra] Stopping original video for source UID', sourceUid);
                  sourceUser.videoTrack.stop();
                }

                // Play Anam avatar video in the source user's container div
                // Use 16:9 wrapper to ensure proper aspect ratio
                playVideoIn16x9Container(user.videoTrack, sourceUid);
              }
            }
          } catch (error) {
            console.error('[Palabra] ❌ Failed to subscribe to UID', uidString + ':', error);
          }
        } else {
          console.log('[Palabra] ℹ️ UID', uidString, 'not requested by this user (ignoring)');
        }
      }
    };

    // Listen directly to Agora SDK client (rtcClient.client is the actual IAgoraRTCClient)
    (rtcClient as any).client.on('user-published', handleUserPublished);

    return () => {
      (rtcClient as any).client.off('user-published', handleUserPublished);
    };
  }, [rtcClient, isTranslationUid, isAnamUid, isPalabraUid, unsubscribeFromUser, playVideoIn16x9Container]);

  // NOTE: No continuous subscription needed - we listen directly to Agora SDK's user-published event above

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
    isAnamUid,
    isTranslationUid,
    // Persistent Avatar Mode
    activeAvatars,
    startAvatar,
    stopAvatar,
    isAvatarActive,
  };

  return (
    <TranslationContext.Provider value={value}>
      {children}
    </TranslationContext.Provider>
  );
};
