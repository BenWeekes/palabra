/**
 * Translation Menu Item Component
 * Displays in the user action menu (3-dot menu) for remote participants
 */

import React, {useState} from 'react';
import {View, Text, StyleSheet, TouchableOpacity, Modal} from 'react-native';
import {UidType} from '../../agora-rn-uikit';
import {useTranslation} from './TranslationProvider';
import {UserActionMenuItem} from '../../src/atoms/ActionMenu';
import ThemeConfig from '../../src/theme';

interface TranslationMenuItemProps {
  closeActionMenu: () => void;
  targetUid: UidType;
  hostMeetingId?: string;
  targetUidType: string;
}

/**
 * This component appears in the user action menu for remote participants
 * Allows starting/stopping translation for a specific user's audio
 */
export const TranslationMenuItem: React.FC<TranslationMenuItemProps> = ({
  closeActionMenu,
  targetUid,
}) => {
  const [showLanguageModal, setShowLanguageModal] = useState(false);
  const {
    isTranslating,
    startTranslation,
    stopTranslation,
    availableLanguages,
  } = useTranslation();

  const uidString = targetUid.toString();
  const translationActive = isTranslating(uidString);

  const handleTranslationClick = () => {
    if (translationActive) {
      // Stop translation
      closeActionMenu();
      stopTranslation(uidString);
    } else {
      // Show language selection modal (don't close menu yet)
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
      // TODO: Show error toast to user
    }
  };

  return (
    <>
      <UserActionMenuItem
        label={translationActive ? 'Stop Translation' : 'Translate Audio'}
        icon="globe"
        iconColor={$config.SECONDARY_ACTION_COLOR}
        textColor={$config.SECONDARY_ACTION_COLOR}
        onPress={handleTranslationClick}
      />

      {/* Compact Language Dropdown */}
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
    marginBottom: 12,
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
