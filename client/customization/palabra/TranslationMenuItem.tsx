/**
 * Translation Menu Item Components
 * Displays in the user action menu (3-dot menu) for remote participants
 * Supports both translation and persistent avatar mode
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
 * Avatar Menu Item - Start/Stop Avatar (persistent mode)
 * Avatar shows with original audio, independent of translation
 */
export const AvatarMenuItem: React.FC<MenuItemProps> = ({
  closeActionMenu,
  targetUid,
}) => {
  const {isAvatarActive, startAvatar, stopAvatar} = useTranslation();

  const uidString = targetUid.toString();
  const avatarActive = isAvatarActive(uidString);

  const handleAvatarClick = async () => {
    closeActionMenu();
    try {
      if (avatarActive) {
        await stopAvatar(uidString);
      } else {
        await startAvatar(uidString);
      }
    } catch (error) {
      console.error('[Palabra] Avatar action failed:', error);
    }
  };

  return (
    <UserActionMenuItem
      label={avatarActive ? 'Stop Avatar' : 'Start Avatar'}
      icon="person"
      iconColor={avatarActive ? '#FF6B6B' : $config.SECONDARY_ACTION_COLOR}
      textColor={avatarActive ? '#FF6B6B' : $config.SECONDARY_ACTION_COLOR}
      onPress={handleAvatarClick}
    />
  );
};

/**
 * Translation Menu Item - Translate Audio
 * When avatar is active, translation switches avatar to translated audio
 */
export const TranslationMenuItem: React.FC<MenuItemProps> = ({
  closeActionMenu,
  targetUid,
}) => {
  const [showLanguageModal, setShowLanguageModal] = useState(false);
  const {
    isTranslating,
    startTranslation,
    stopTranslation,
    availableLanguages,
    isAvatarActive,
  } = useTranslation();

  const uidString = targetUid.toString();
  const translationActive = isTranslating(uidString);
  const avatarActive = isAvatarActive(uidString);

  const handleTranslationClick = () => {
    if (translationActive) {
      closeActionMenu();
      stopTranslation(uidString);
    } else {
      setShowLanguageModal(true);
    }
  };

  const handleLanguageSelect = async (languageCode: string) => {
    setShowLanguageModal(false);
    closeActionMenu();

    try {
      await startTranslation(
        uidString,
        'auto', // Palabra auto-detects source language
        languageCode,
      );
    } catch (error) {
      console.error('[Palabra] Failed to start translation:', error);
    }
  };

  return (
    <>
      <UserActionMenuItem
        label={translationActive ? 'Stop Translation' : 'Translate Audio'}
        icon="globe"
        iconColor={translationActive ? '#4ECDC4' : $config.SECONDARY_ACTION_COLOR}
        textColor={translationActive ? '#4ECDC4' : $config.SECONDARY_ACTION_COLOR}
        onPress={handleTranslationClick}
      />

      {/* Language Selection Modal */}
      <Modal
        visible={showLanguageModal}
        transparent={true}
        animationType="fade"
        onRequestClose={() => {
          setShowLanguageModal(false);
          closeActionMenu();
        }}>
        <TouchableOpacity
          style={styles.modalOverlay}
          activeOpacity={1}
          onPress={() => {
            setShowLanguageModal(false);
            closeActionMenu();
          }}>
          <View style={styles.dropdownContainer}>
            <Text style={styles.dropdownTitle}>Translate to:</Text>
            {avatarActive && (
              <Text style={styles.avatarHint}>
                Avatar will switch to translated audio
              </Text>
            )}
            <View style={styles.languageGrid}>
              {availableLanguages.map(lang => (
                <TouchableOpacity
                  key={lang.code}
                  style={styles.languageOption}
                  onPress={() => handleLanguageSelect(lang.code)}>
                  <Text style={styles.languageFlag}>{lang.flag}</Text>
                  <Text style={styles.languageName}>{lang.name}</Text>
                </TouchableOpacity>
              ))}
            </View>
          </View>
        </TouchableOpacity>
      </Modal>
    </>
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
  avatarHint: {
    fontSize: ThemeConfig.FontSize.small,
    color: '#4ECDC4',
    fontFamily: ThemeConfig.FontFamily.sansPro,
    marginBottom: 12,
    fontStyle: 'italic',
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
