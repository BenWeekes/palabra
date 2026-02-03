/**
 * Translation Menu Item Components — 4 Translation Modes + Stop
 * Displays in the user action menu (3-dot menu) for remote participants
 */

import React, {useState} from 'react';
import {View, Text, StyleSheet, TouchableOpacity, Modal} from 'react-native';
import {UidType} from '../../agora-rn-uikit';
import {useTranslation} from './TranslationProvider';
import {UserActionMenuItem} from '../../src/atoms/ActionMenu';
import ThemeConfig from '../../src/theme';

interface MenuItemProps {
  closeActionMenu: () => void;
  targetUid: UidType;
  hostMeetingId?: string;
  targetUidType: string;
}

/**
 * Shared language picker modal used by all 4 mode-start components
 */
const LanguagePickerModal: React.FC<{
  visible: boolean;
  title: string;
  onSelect: (langCode: string) => void;
  onClose: () => void;
}> = ({visible, title, onSelect, onClose}) => {
  const {availableLanguages} = useTranslation();

  return (
    <Modal
      visible={visible}
      transparent={true}
      animationType="fade"
      onRequestClose={onClose}>
      <TouchableOpacity
        style={styles.modalOverlay}
        activeOpacity={1}
        onPress={onClose}>
        <View style={styles.dropdownContainer}>
          <Text style={styles.dropdownTitle}>{title}</Text>
          <View style={styles.languageGrid}>
            {availableLanguages.map(lang => (
              <TouchableOpacity
                key={lang.code}
                style={styles.languageOption}
                onPress={() => onSelect(lang.code)}>
                <Text style={styles.languageFlag}>{lang.flag}</Text>
                <Text style={styles.languageName}>{lang.name}</Text>
              </TouchableOpacity>
            ))}
          </View>
        </View>
      </TouchableOpacity>
    </Modal>
  );
};

/**
 * Mode 1: Start Avatar (persistent avatar + translated audio via avatar)
 */
export const StartAvatarMenuItem: React.FC<MenuItemProps> = ({
  closeActionMenu,
  targetUid,
}) => {
  const {isTranslating, isAvatarActive, startAvatar, startTranslation} =
    useTranslation();
  const [showLanguageModal, setShowLanguageModal] = useState(false);

  const uid = targetUid.toString();
  const anyActive = isTranslating(uid) || isAvatarActive(uid);

  const handleClick = () => {
    if (anyActive) return;
    setShowLanguageModal(true);
  };

  const handleLanguageSelect = async (lang: string) => {
    setShowLanguageModal(false);
    closeActionMenu();
    try {
      await startAvatar(uid);
      await startTranslation(uid, 'auto', lang, 'avatar');
    } catch (error) {
      console.error('[Palabra] Start Avatar failed:', error);
    }
  };

  return (
    <>
      <UserActionMenuItem
        label="Start Avatar"
        icon="person"
        iconColor={anyActive ? '#666' : $config.SECONDARY_ACTION_COLOR}
        textColor={anyActive ? '#666' : $config.SECONDARY_ACTION_COLOR}
        disabled={anyActive}
        onPress={handleClick}
      />
      <LanguagePickerModal
        visible={showLanguageModal}
        title="Start Avatar — Translate to:"
        onSelect={handleLanguageSelect}
        onClose={() => {
          setShowLanguageModal(false);
          closeActionMenu();
        }}
      />
    </>
  );
};

/**
 * Mode 2: Translate with Video (non-persistent avatar + translated audio)
 */
export const TranslateWithVideoMenuItem: React.FC<MenuItemProps> = ({
  closeActionMenu,
  targetUid,
}) => {
  const {isTranslating, isAvatarActive, startTranslation} = useTranslation();
  const [showLanguageModal, setShowLanguageModal] = useState(false);

  const uid = targetUid.toString();
  const anyActive = isTranslating(uid) || isAvatarActive(uid);

  const handleClick = () => {
    if (anyActive) return;
    setShowLanguageModal(true);
  };

  const handleLanguageSelect = async (lang: string) => {
    setShowLanguageModal(false);
    closeActionMenu();
    try {
      await startTranslation(uid, 'auto', lang, 'translate_video');
    } catch (error) {
      console.error('[Palabra] Translate with Video failed:', error);
    }
  };

  return (
    <>
      <UserActionMenuItem
        label="Translate with Video"
        icon="video-on"
        iconColor={anyActive ? '#666' : $config.SECONDARY_ACTION_COLOR}
        textColor={anyActive ? '#666' : $config.SECONDARY_ACTION_COLOR}
        disabled={anyActive}
        onPress={handleClick}
      />
      <LanguagePickerModal
        visible={showLanguageModal}
        title="Translate with Video — Translate to:"
        onSelect={handleLanguageSelect}
        onClose={() => {
          setShowLanguageModal(false);
          closeActionMenu();
        }}
      />
    </>
  );
};

/**
 * Mode 3: Translate Audio (audio-only translation, original muted)
 */
export const TranslateAudioMenuItem: React.FC<MenuItemProps> = ({
  closeActionMenu,
  targetUid,
}) => {
  const {isTranslating, isAvatarActive, startTranslation} = useTranslation();
  const [showLanguageModal, setShowLanguageModal] = useState(false);

  const uid = targetUid.toString();
  const anyActive = isTranslating(uid) || isAvatarActive(uid);

  const handleClick = () => {
    if (anyActive) return;
    setShowLanguageModal(true);
  };

  const handleLanguageSelect = async (lang: string) => {
    setShowLanguageModal(false);
    closeActionMenu();
    try {
      await startTranslation(uid, 'auto', lang, 'translate_audio');
    } catch (error) {
      console.error('[Palabra] Translate Audio failed:', error);
    }
  };

  return (
    <>
      <UserActionMenuItem
        label="Translate Audio"
        icon="globe"
        iconColor={anyActive ? '#666' : $config.SECONDARY_ACTION_COLOR}
        textColor={anyActive ? '#666' : $config.SECONDARY_ACTION_COLOR}
        disabled={anyActive}
        onPress={handleClick}
      />
      <LanguagePickerModal
        visible={showLanguageModal}
        title="Translate Audio — Translate to:"
        onSelect={handleLanguageSelect}
        onClose={() => {
          setShowLanguageModal(false);
          closeActionMenu();
        }}
      />
    </>
  );
};

/**
 * Mode 4: Translate Audio + Original (translated at full vol, original at 20%)
 */
export const TranslateAudioOriginalMenuItem: React.FC<MenuItemProps> = ({
  closeActionMenu,
  targetUid,
}) => {
  const {isTranslating, isAvatarActive, startTranslation} = useTranslation();
  const [showLanguageModal, setShowLanguageModal] = useState(false);

  const uid = targetUid.toString();
  const anyActive = isTranslating(uid) || isAvatarActive(uid);

  const handleClick = () => {
    if (anyActive) return;
    setShowLanguageModal(true);
  };

  const handleLanguageSelect = async (lang: string) => {
    setShowLanguageModal(false);
    closeActionMenu();
    try {
      await startTranslation(uid, 'auto', lang, 'translate_audio_with_original');
    } catch (error) {
      console.error('[Palabra] Translate Audio + Original failed:', error);
    }
  };

  return (
    <>
      <UserActionMenuItem
        label="Translate Audio + Original"
        icon="speaker"
        iconColor={anyActive ? '#666' : $config.SECONDARY_ACTION_COLOR}
        textColor={anyActive ? '#666' : $config.SECONDARY_ACTION_COLOR}
        disabled={anyActive}
        onPress={handleClick}
      />
      <LanguagePickerModal
        visible={showLanguageModal}
        title="Translate Audio + Original — Translate to:"
        onSelect={handleLanguageSelect}
        onClose={() => {
          setShowLanguageModal(false);
          closeActionMenu();
        }}
      />
    </>
  );
};

/**
 * Stop Translation — stops any active mode
 */
export const StopTranslationMenuItem: React.FC<MenuItemProps> = ({
  closeActionMenu,
  targetUid,
}) => {
  const {isTranslating, isAvatarActive, stopTranslation, stopAvatar} =
    useTranslation();

  const uid = targetUid.toString();
  const anyActive = isTranslating(uid) || isAvatarActive(uid);

  const handleStop = async () => {
    if (!anyActive) return;
    closeActionMenu();
    try {
      if (isTranslating(uid)) await stopTranslation(uid);
      if (isAvatarActive(uid)) await stopAvatar(uid);
    } catch (error) {
      console.error('[Palabra] Stop Translation failed:', error);
    }
  };

  return (
    <UserActionMenuItem
      label="Stop Translation"
      icon="close"
      iconColor={anyActive ? '#FF6B6B' : '#666'}
      textColor={anyActive ? '#FF6B6B' : '#666'}
      disabled={!anyActive}
      onPress={handleStop}
    />
  );
};

const styles = StyleSheet.create({
  modalOverlay: {
    flex: 1,
    backgroundColor: 'rgba(0, 0, 0, 0.5)',
    justifyContent: 'center',
    alignItems: 'center',
  },
  dropdownContainer: {
    backgroundColor: $config.CARD_LAYER_4_COLOR,
    borderRadius: 8,
    padding: 16,
    width: 340,
    shadowColor: '#000',
    shadowOffset: {width: 0, height: 4},
    shadowOpacity: 0.3,
    shadowRadius: 8,
    elevation: 8,
  },
  dropdownTitle: {
    fontSize: ThemeConfig.FontSize.normal,
    fontWeight: '600',
    color: $config.FONT_COLOR,
    fontFamily: ThemeConfig.FontFamily.sansPro,
    marginBottom: 8,
  },
  languageGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
  },
  languageOption: {
    flexDirection: 'column',
    alignItems: 'center',
    padding: 8,
    borderRadius: 6,
    backgroundColor: $config.CARD_LAYER_3_COLOR,
    borderWidth: 1,
    borderColor: $config.CARD_LAYER_5_COLOR,
    width: 96,
    marginRight: 6,
    marginBottom: 6,
  },
  languageName: {
    fontSize: ThemeConfig.FontSize.small,
    color: $config.FONT_COLOR,
    fontWeight: '500',
    fontFamily: ThemeConfig.FontFamily.sansPro,
    textAlign: 'center',
  },
  languageFlag: {
    fontSize: 32,
    marginBottom: 4,
  },
});
