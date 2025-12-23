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
        'en', // TODO: Make source language configurable or auto-detect
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
          <TouchableOpacity
            activeOpacity={1}
            onPress={e => e.stopPropagation()}>
            <View style={styles.modalContent}>
              <View style={styles.modalHeader}>
                <Text style={styles.modalTitle}>
                  Select Translation Language
                </Text>
                <TouchableOpacity
                  onPress={() => {
                    setShowLanguageModal(false);
                    closeActionMenu();
                  }}
                  style={styles.closeButton}>
                  <Text style={styles.closeButtonText}>✕</Text>
                </TouchableOpacity>
              </View>

              <View style={styles.languageList}>
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
  modalContent: {
    backgroundColor: $config.CARD_LAYER_4_COLOR,
    borderRadius: 8,
    padding: 24,
    minWidth: 320,
    maxWidth: 400,
    maxHeight: '80%',
    shadowColor: '#000',
    shadowOffset: {width: 0, height: 4},
    shadowOpacity: 0.3,
    shadowRadius: 8,
    elevation: 8,
  },
  modalHeader: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 20,
  },
  modalTitle: {
    fontSize: ThemeConfig.FontSize.large,
    fontWeight: '600',
    color: $config.FONT_COLOR,
    fontFamily: ThemeConfig.FontFamily.sansPro,
  },
  closeButton: {
    padding: 4,
  },
  closeButtonText: {
    fontSize: 24,
    color: $config.SECONDARY_ACTION_COLOR,
    fontWeight: '300',
  },
  languageList: {
    gap: 10,
  },
  languageOption: {
    flexDirection: 'row',
    alignItems: 'center',
    padding: 14,
    borderRadius: 8,
    backgroundColor: $config.CARD_LAYER_3_COLOR,
    borderWidth: 1,
    borderColor: $config.CARD_LAYER_5_COLOR,
  },
  languageName: {
    fontSize: ThemeConfig.FontSize.normal,
    color: $config.FONT_COLOR,
    fontWeight: '500',
    fontFamily: ThemeConfig.FontFamily.sansPro,
  },
  languageFlag: {
    fontSize: 28,
    marginRight: 12,
  },
});
